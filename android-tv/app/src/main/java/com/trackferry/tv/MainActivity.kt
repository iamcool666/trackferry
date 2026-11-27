package com.trackferry.tv

import android.content.ComponentName
import android.content.Intent
import android.os.Bundle
import android.os.SystemClock
import android.widget.Toast
import androidx.activity.ComponentActivity
import androidx.activity.compose.BackHandler
import androidx.activity.compose.setContent
import androidx.core.content.ContextCompat
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.unit.sp
import androidx.tv.material3.Text
import androidx.lifecycle.lifecycleScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.media3.session.MediaController
import androidx.media3.session.SessionToken
import com.google.common.util.concurrent.ListenableFuture

class MainActivity : ComponentActivity() {
    private enum class Page { Loading, Setup, Library, Player, Demo }
    private var page by mutableStateOf(Page.Loading)
    /** The opening plays once per launch, over the app that loads underneath. */
    private var intro by mutableStateOf(false)
    private var nas by mutableStateOf<NasConnection?>(null)
    private var browse = TvBrowseState()
    private var saved by mutableStateOf<TvLink?>(null)
    private var saveFailed by mutableStateOf(false)
    private val credentials by lazy { TvCredentials(applicationContext) }
    private var connection: ListenableFuture<MediaController>? = null
    private var controller by mutableStateOf<MediaController?>(null)
    private var connectionFailed by mutableStateOf(false)
    private var backAt = 0L
    private var backHint: Toast? = null

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        intro = savedInstanceState == null
        setContent {
            TrackFerryScreen {
                val player = controller
                BackHandler(page == Page.Library || page == Page.Setup) {
                    if (page == Page.Setup && nas != null) page = Page.Library
                    else leave()
                }
                BackHandler(page == Page.Player || page == Page.Demo) { backFromPlayer() }
                when {
                    player == null && connectionFailed -> TvButton("重新连接播放器", onClick = ::connect)
                    player == null || page == Page.Loading -> Text("正在连接播放器…", color = Muted, fontSize = 20.sp)
                    page == Page.Setup -> TvSetupScreen(::paired, { startDemo(player) }, saved, ::forget,
                        onBack = if (nas != null) ({ page = Page.Library }) else null)
                    page == Page.Library -> nas?.let { current ->
                        TvLibraryScreen(current, browse, ::startNas, player,
                            { page = Page.Player },
                            { page = Page.Setup }, ::forget)
                    }
                    else -> PlayerScreen(player, ::backFromPlayer) { browse.shelf = null; browse.tracks = null }
                }
                if (saveFailed) Text("无法保存电视配对", color = Accent, fontSize = 16.sp)
                if (intro) Intro { intro = false }
            }
            LaunchedEffect(Unit) { NasPlayback.revoked.collect { if (it) { forget(); NasPlayback.revoked.value = false } } }
            LaunchedEffect(Unit) {
                NasPlayback.relocated.collect { current ->
                    if (current != null && saved?.token == current.link.token) {
                        saved = current.link
                        nas = current
                        if (page == Page.Setup) page = Page.Library
                    }
                }
            }
            LaunchedEffect(Unit) { NasPlayback.remoteSelection.collect { if (it != null && nas != null && NasPlayback.visible) { page = Page.Player; NasPlayback.remoteSelection.value = null } } }
            LaunchedEffect(Unit) {
                saved = withContext(Dispatchers.IO) { credentials.load() }
                val previous = saved
                if (previous != null) {
                    try {
                        val server = withContext(Dispatchers.IO) { TvClient.probe(previous.server.address, previous.server.pin) }
                        useLink(previous.copy(server = server))
                    } catch (e: CancellationException) { throw e }
                    catch (_: Exception) { page = Page.Setup }
                } else page = Page.Setup
            }
        }
    }

    override fun onStart() {
        super.onStart()
        NasPlayback.visible = true
        if (nas != null && NasPlayback.remoteSelection.value != null) { page = Page.Player; NasPlayback.remoteSelection.value = null }
        connect()
    }

    override fun onStop() {
        NasPlayback.visible = false
        disconnect()
        super.onStop()
    }

    private fun connect() {
        disconnect()
        connectionFailed = false
        val token = SessionToken(this, ComponentName(this, PlaybackService::class.java))
        val pending = MediaController.Builder(this, token).setListener(object : MediaController.Listener {
            override fun onDisconnected(disconnected: MediaController) {
                if (controller === disconnected) {
                    disconnect()
                    connectionFailed = true
                }
            }
        }).buildAsync()
        connection = pending
        pending.addListener({
            if (connection === pending) {
                controller = runCatching { pending.get() }.getOrElse {
                    connectionFailed = true
                    null
                }

            }
        }, ContextCompat.getMainExecutor(this))
    }

    private fun paired(link: TvLink) {
        lifecycleScope.launch {
            try {
                withContext(Dispatchers.IO) { credentials.save(link) }
                saveFailed = false
                useLink(link)
            } catch (e: CancellationException) { throw e }
            catch (_: Exception) { saveFailed = true }
        }
    }

    private fun useLink(link: TvLink) {
        if (saved?.server?.pin != link.server.pin) browse = TvBrowseState()
        saved = link
        val current = NasConnection(link)
        nas = current
        NasPlayback.connection = current
        page = Page.Library
    }

    private fun forget() {
        controller?.let { it.stop(); it.clearMediaItems() }
        credentials.clear()
        saved = null
        nas = null
        browse = TvBrowseState()
        NasPlayback.connection = null
        page = Page.Setup
    }

    private fun backFromPlayer() { page = if (page == Page.Demo) Page.Setup else Page.Library }

    /**
     * Back on the home page: the first press says what a second one does, the second within two
     * seconds stops the music and quits. To leave it playing, the remote's Home key.
     */
    private fun leave() {
        val now = SystemClock.elapsedRealtime()
        if (now - backAt < 2_000) {
            backAt = 0
            backHint?.cancel()
            exit()
            return
        }
        backAt = now
        backHint?.cancel()
        val playing = controller?.let { it.isPlaying || it.playWhenReady && it.mediaItemCount > 0 } == true
        backHint = Toast.makeText(this, if (playing) "再按一次返回退出并停止播放；想让音乐接着放，按主页键" else "再按一次返回退出", Toast.LENGTH_SHORT).also { it.show() }
    }

    private fun exit() {
        NasPlayback.visible = false
        NasPlayback.remoteSelection.value = null
        PlaybackService.stopPlayback?.invoke()
        disconnect()
        stopService(Intent(this, PlaybackService::class.java))
        finishAndRemoveTask()
    }

    private fun startDemo(player: MediaController) {
        player.setMediaItems(Tracks.map { it.mediaItem(packageName) })
        player.prepare()
        page = Page.Demo
    }

    private fun startNas(tracks: List<NasTrack>, index: Int, fullscreen: Boolean): Boolean {
        if (PlaybackService.playCatalog?.invoke(tracks, index) != true) return false
        if (fullscreen) page = Page.Player
        return true
    }

    private fun disconnect() {
        val previous = connection
        connection = null
        controller = null
        if (previous != null) MediaController.releaseFuture(previous)
    }
}
