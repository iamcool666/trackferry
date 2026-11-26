package com.trackferry.tv

import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.pm.PackageInstaller
import android.net.Uri
import android.os.Build
import android.provider.Settings
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.withContext
import java.io.File
import java.io.IOException

/**
 * Brings the television's app up to the server's version. The server fetches the release, checks
 * its signature (internal/update/tv.go) and keeps it; here the file is fetched from the server,
 * its sum is checked again, and Android installs it. Every step can fail and be tried again.
 */
internal object TvUpdate {
    sealed interface Step {
        data object Idle : Step
        data object Checking : Step
        data class Current(val version: String) : Step
        data class Available(val version: String, val size: Long) : Step
        /** The server is fetching and checking the release. */
        data class Preparing(val version: String, val done: Long, val total: Long) : Step
        data class Downloading(val version: String, val done: Long, val total: Long) : Step
        data class Installing(val version: String) : Step
        /** Android needs this app to be allowed to install apps. */
        data class Permission(val version: String) : Step
        data class Failed(val message: String, val version: String = "") : Step
    }

    private val state = MutableStateFlow<Step>(Step.Idle)
    val step: StateFlow<Step> = state
    /** Outlives the page, so leaving 设置 does not stop an update half way. */
    private val scope by lazy { CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate) }
    private var job: Job? = null

    fun launch(block: suspend () -> Unit) {
        if (job?.isActive == true) return
        job = scope.launch { block() }
    }

    /** The ABI this television runs best, of the two the release is built for. */
    val abi: String get() = Build.SUPPORTED_ABIS.firstOrNull { it == "arm64-v8a" || it == "armeabi-v7a" } ?: "armeabi-v7a"

    fun installed(context: Context): String =
        runCatching { context.packageManager.getPackageInfo(context.packageName, 0).versionName }.getOrNull().orEmpty()

    /** True when [server] is a later release than [mine]; anything that does not read as a version is not. */
    fun newer(server: String, mine: String): Boolean {
        fun parts(v: String): List<Int>? = v.trim().removePrefix("v").substringBefore('-').split('.').map { it.toIntOrNull() ?: return null }
        val a = parts(server) ?: return false
        val b = parts(mine) ?: return true
        for (i in 0 until maxOf(a.size, b.size)) {
            val x = a.getOrElse(i) { 0 }
            val y = b.getOrElse(i) { 0 }
            if (x != y) return x > y
        }
        return false
    }

    suspend fun check(context: Context, nas: NasConnection) {
        if (busy()) return
        state.value = Step.Checking
        state.value = try {
            val app = withContext(Dispatchers.IO) { nas.appStatus(abi) }
            when {
                app.version.isBlank() -> Step.Failed("服务器没说它是哪个版本")
                !newer(app.version, installed(context)) -> Step.Current(app.version)
                app.phase == "download" -> Step.Preparing(app.version, app.done, app.total)
                app.phase == "failed" -> Step.Failed(app.error.ifBlank { "服务器没能取到新版" }, app.version)
                else -> Step.Available(app.version, app.size)
            }
        } catch (e: CancellationException) { state.value = Step.Idle; throw e }
        catch (e: Exception) { Step.Failed(e.tvMessage("暂时查不到新版本")) }
    }

    private fun busy() = state.value.let { it is Step.Checking || it is Step.Preparing || it is Step.Downloading || it is Step.Installing }

    /** Has the server get the release ready, fetches it and hands it to Android. */
    suspend fun install(context: Context, nas: NasConnection) {
        val app = context.applicationContext
        var version = ""
        try {
            // 1. The server fetches and checks the release; ask until it says it is ready.
            var status = withContext(Dispatchers.IO) { nas.prepareApp(abi) }
            version = status.version
            var quiet = 0
            while (status.phase != "ready") {
                if (status.phase == "failed") throw IOException(status.error.ifBlank { "服务器没能取到新版" })
                state.value = Step.Preparing(status.version, status.done, status.total)
                delay(1_200)
                status = try { withContext(Dispatchers.IO) { nas.appStatus(abi) } }
                catch (e: CancellationException) { throw e }
                catch (e: IOException) { if (++quiet > 8) throw e; continue }
                // An empty phase means the server forgot the job (it restarted): ask it again.
                if (status.phase.isEmpty()) status = withContext(Dispatchers.IO) { nas.prepareApp(abi) }
            }
            version = status.version
            if (!newer(version, installed(app))) { state.value = Step.Current(version); return }
            // 2. Fetch it from the server, checking the sum, a few times if the network drops.
            val dir = File(app.cacheDir, "update").apply { mkdirs() }
            dir.listFiles()?.forEach { it.delete() }
            val file = File(dir, "trackferry-tv-$version-$abi.apk")
            var attempt = 0
            while (true) {
                try {
                    state.value = Step.Downloading(version, 0, status.size)
                    withContext(Dispatchers.IO) {
                        nas.downloadApp(abi, file, status.sha256) { done, total -> state.value = Step.Downloading(version, done, if (total > 0) total else status.size) }
                    }
                    break
                } catch (e: CancellationException) { throw e }
                catch (e: IOException) {
                    file.delete()
                    if (e is TvHttpError && e.status in 400..499 && e.status != 408 && e.status != 429) throw e
                    if (++attempt >= 3) throw e
                    delay(2_000L * attempt)
                }
            }
            // 3. Hand it to Android.
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O && !app.packageManager.canRequestPackageInstalls()) {
                state.value = Step.Permission(version)
                return
            }
            state.value = Step.Installing(version)
            withContext(Dispatchers.IO) { commit(app, file) }
        } catch (e: CancellationException) {
            if (state.value !is Step.Installing) state.value = Step.Idle
            throw e
        } catch (e: Exception) {
            state.value = Step.Failed(e.tvMessage(e.message?.takeIf { it.isNotBlank() && e is IOException } ?: "更新没有完成"), version)
        }
    }

    /** After the permission was granted: install the file already fetched, or start over. */
    suspend fun resume(context: Context, nas: NasConnection) {
        val waiting = state.value as? Step.Permission ?: return
        val app = context.applicationContext
        val file = File(File(app.cacheDir, "update"), "trackferry-tv-${waiting.version}-$abi.apk")
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O && !app.packageManager.canRequestPackageInstalls()) return
        if (!file.isFile) { state.value = Step.Idle; install(app, nas); return }
        state.value = Step.Installing(waiting.version)
        try { withContext(Dispatchers.IO) { commit(app, file) } }
        catch (e: CancellationException) { throw e }
        catch (e: Exception) { state.value = Step.Failed("交给系统安装时出错了", waiting.version) }
    }

    /** Opens the screen where this app may be allowed to install apps; false when the television has none. */
    fun askPermission(context: Context): Boolean {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return false
        return runCatching {
            context.startActivity(Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, Uri.parse("package:${context.packageName}"))
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        }.isSuccess
    }

    private fun commit(context: Context, file: File) {
        val installer = context.packageManager.packageInstaller
        // Sessions left over from an attempt that did not finish would only take up room.
        installer.mySessions.forEach { runCatching { installer.abandonSession(it.sessionId) } }
        val params = PackageInstaller.SessionParams(PackageInstaller.SessionParams.MODE_FULL_INSTALL).apply {
            setAppPackageName(context.packageName)
            setSize(file.length())
        }
        val id = installer.createSession(params)
        try {
            installer.openSession(id).use { session ->
                session.openWrite("trackferry.apk", 0, file.length()).use { out ->
                    file.inputStream().use { it.copyTo(out, 64 * 1024) }
                    session.fsync(out)
                }
                val flags = PendingIntent.FLAG_UPDATE_CURRENT or if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) PendingIntent.FLAG_MUTABLE else 0
                val intent = PendingIntent.getBroadcast(context, id, Intent(context, InstallResult::class.java).setPackage(context.packageName), flags)
                session.commit(intent.intentSender)
            }
        } catch (e: Exception) {
            runCatching { installer.abandonSession(id) }
            throw e
        }
    }

    internal fun report(status: Int, message: String?) {
        val version = (state.value as? Step.Installing)?.version.orEmpty()
        state.value = when (status) {
            PackageInstaller.STATUS_SUCCESS -> Step.Current(version)
            PackageInstaller.STATUS_FAILURE_ABORTED -> Step.Available(version, 0)
            PackageInstaller.STATUS_FAILURE_CONFLICT -> Step.Failed("新版和装着的这个签名不同，请先卸载再装", version)
            PackageInstaller.STATUS_FAILURE_STORAGE -> Step.Failed("电视存储空间不够", version)
            PackageInstaller.STATUS_FAILURE_INCOMPATIBLE -> Step.Failed("这台电视装不了这个版本", version)
            else -> Step.Failed(message?.takeIf { it.isNotBlank() }?.let { "系统没有装上：$it" } ?: "系统没有装上新版", version)
        }
    }
}

/** Android's answer about the install; when it needs the viewer to confirm, it shows its own screen. */
class InstallResult : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        val status = intent.getIntExtra(PackageInstaller.EXTRA_STATUS, PackageInstaller.STATUS_FAILURE)
        if (status == PackageInstaller.STATUS_PENDING_USER_ACTION) {
            @Suppress("DEPRECATION")
            val confirm = intent.getParcelableExtra<Intent>(Intent.EXTRA_INTENT)
            if (confirm == null || runCatching { context.startActivity(confirm.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)) }.isFailure)
                TvUpdate.report(PackageInstaller.STATUS_FAILURE, "没能打开系统的安装确认")
            return
        }
        TvUpdate.report(status, intent.getStringExtra(PackageInstaller.EXTRA_STATUS_MESSAGE))
    }
}
