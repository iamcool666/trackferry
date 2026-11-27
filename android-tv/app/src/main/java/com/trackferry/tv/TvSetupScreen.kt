package com.trackferry.tv

import android.os.Build
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.Alignment
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.focusProperties
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalSoftwareKeyboardController
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.tv.material3.Text
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import okhttp3.HttpUrl

@Composable
internal fun TvSetupScreen(onPaired: (TvLink) -> Unit, demo: () -> Unit, saved: TvLink? = null, onForget: () -> Unit = {}, onBack: (() -> Unit)? = null) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var scan by remember { mutableStateOf(Scan()) }
    var sweep by remember { mutableIntStateOf(0) }
    var settled by remember { mutableStateOf(false) }
    var manual by remember { mutableStateOf(false) }
    var entered by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var attempt by remember { mutableStateOf<PairAttempt?>(null) }
    var error by remember { mutableStateOf("") }
    val first = remember { FocusRequester() }
    val found = remember { FocusRequester() }

    LaunchedEffect(sweep, attempt) {
        if (attempt != null) return@LaunchedEffect
        settled = false
        scan = Scan()
        discover(context).collect { scan = it }
    }
    LaunchedEffect(sweep, attempt) { if (attempt == null) { delay(4_000); settled = true } }
    LaunchedEffect(attempt != null, manual) { first.requestFocus() }
    fun cancelAttempt() {
        val previous = attempt ?: return
        attempt = null
        error = ""
        scope.launch(Dispatchers.IO) { runCatching { TvClient.cancel(previous) } }
    }
    BackHandler(attempt != null || manual) { if (attempt != null) cancelAttempt() else manual = false }

    val choose: (HttpUrl, String?) -> Unit = { address, expected ->
        if (!busy) scope.launch {
            busy = true
            error = ""
            try {
                val server = withContext(Dispatchers.IO) { TvClient.probe(address, saved?.server?.pin ?: expected) }
                if (saved == null) attempt = withContext(Dispatchers.IO) {
                    TvClient.begin(server, Build.MODEL.take(64).ifBlank { "Android TV" })
                } else onPaired(saved.copy(server = server))
            } catch (e: CancellationException) { throw e }
            catch (e: Exception) { error = e.tvMessage("无法连接 TrackFerry") }
            finally { busy = false }
        }
    }
    // The first server found takes the focus once, so OK connects to it straight away.
    val any = attempt == null && !manual && scan.servers.isNotEmpty()
    LaunchedEffect(any) { if (any) { delay(60); runCatching { found.requestFocus() } } }
    val auto = scan.servers.firstOrNull { saved != null && it.pin == saved.server.pin && it.address != saved.server.address }
    LaunchedEffect(auto) { if (auto != null && !busy) choose(auto.address, auto.pin) }
    LaunchedEffect(attempt) {
        val current = attempt ?: return@LaunchedEffect
        val completed = withTimeoutOrNull(300_000) {
            while (true) {
                try {
                    if (withContext(Dispatchers.IO) { TvClient.awaitApproval(current) }) return@withTimeoutOrNull true
                } catch (e: TvHttpError) {
                    if (e.status == 410) return@withTimeoutOrNull false
                    error = e.tvMessage("正在重新连接")
                } catch (e: CancellationException) { throw e }
                catch (_: Exception) { error = "连接暂时中断，正在重试" }
                delay(2_000)
            }
            @Suppress("UNREACHABLE_CODE")
            false
        }
        if (completed == true) onPaired(TvLink(current.server, current.token))
        else { attempt = null; error = "配对码已过期，请重新选择服务器" }
    }

    Column(Modifier.fillMaxWidth(.9f).padding(vertical = 18.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween, verticalAlignment = Alignment.CenterVertically) {
            FerryBrand()
            onBack?.let { StageNav("返回设置", TvGlyph.Back, onClick = it) }
        }
        StagePanel(Modifier.fillMaxWidth()) {
            Column(verticalArrangement = Arrangement.spacedBy(9.dp)) {
                if (attempt != null) {
                    PairCode(attempt!!, error, Modifier.focusRequester(first), ::cancelAttempt)
                } else {
                    Text(if (saved == null) "连接你的曲库" else "连接设置", color = Ink, fontSize = 30.sp, fontWeight = FontWeight.SemiBold)
                    if (saved != null) Text("${saved.server.name} · ${saved.server.address.host}", color = Teal, fontSize = 15.sp)
                    val servers = scan.servers.filter { saved == null || it.pin == saved.server.pin }.distinctBy { it.pin ?: it.address.toString() }
                    Text(when {
                        busy -> "正在连接…"
                        scan.failed -> "暂时无法搜索局域网，可以手动连接。"
                        servers.size == 1 -> "在局域网里找到了它，按确认连接"
                        servers.isNotEmpty() -> "在局域网里找到 ${servers.size} 台，选一台连接"
                        settled -> "暂未发现服务器。请确认电视和 NAS 在同一网络。"
                        else -> "正在搜索局域网中的 TrackFerry…"
                    }, color = Muted, fontSize = 18.sp)
                    if (manual) {
                        val connect = remember { FocusRequester() }
                        val submit = {
                            val address = serverAddress(entered)
                            if (address == null) error = "请输入 NAS 主机名或地址，可带端口"
                            else choose(address, null)
                        }
                        TvInput(entered, { entered = it }, "NAS 地址", Modifier.focusRequester(first).focusProperties { down = connect }, submit = submit)
                        TvButton("连接", Modifier.focusRequester(connect), enabled = !busy && entered.isNotBlank(), onClick = submit)
                    } else {
                        if (servers.isNotEmpty()) Column(Modifier.padding(top = 6.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                            servers.take(4).forEachIndexed { i, target ->
                                ServerCard(target, !busy, if (i == 0) Modifier.focusRequester(found) else Modifier) { choose(target.address, target.pin) }
                            }
                        }
                        Row(Modifier.padding(top = 4.dp), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                            TvButton(if (servers.isEmpty()) "重新搜索" else "再搜一次", Modifier.focusRequester(first), !busy) { error = ""; sweep++ }
                            TvButton("手动连接", enabled = !busy) { manual = true }
                            if (saved == null) TvButton("试听验证曲", onClick = demo)
                            else TvButton("换一台服务器", onClick = onForget)
                        }
                    }
                }
                if (error.isNotBlank() && attempt == null) Text(error, color = Accent, fontSize = 17.sp)
            }
        }
    }
}

/** A server on the network: the fish, the NAS's own name, where it is and which version it runs. */
@Composable
private fun ServerCard(target: Discovered, enabled: Boolean, modifier: Modifier, onClick: () -> Unit) {
    var focused by remember { mutableStateOf(false) }
    StageTile(modifier.fillMaxWidth().height(86.dp).onFocusChanged { focused = it.isFocused }, enabled = enabled, onClick = onClick) {
        Row(Modifier.fillMaxSize().padding(horizontal = 18.dp), horizontalArrangement = Arrangement.spacedBy(16.dp), verticalAlignment = Alignment.CenterVertically) {
            Box(Modifier.size(54.dp).clip(CircleShape)
                .background(androidx.compose.ui.graphics.Brush.linearGradient(listOf(FishLow.copy(alpha = .30f), FishHigh.copy(alpha = .26f)))),
                contentAlignment = Alignment.Center) {
                androidx.compose.foundation.Image(androidx.compose.ui.res.painterResource(R.drawable.fish_mark), null, Modifier.size(40.dp))
            }
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
                Text(target.title, color = Ink, fontSize = 22.sp, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(listOfNotNull(target.address.host, target.version.takeIf { it.isNotBlank() }?.let { "TrackFerry $it" }).joinToString("  ·  "),
                    color = Muted, fontSize = 14.sp, maxLines = 1)
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                Text("连接", color = if (focused) Accent else Muted.copy(alpha = .7f), fontSize = 16.sp, fontWeight = FontWeight.Medium)
                StageIcon(TvGlyph.Play, Modifier.size(14.dp), if (focused) Accent else Muted.copy(alpha = .6f))
            }
        }
    }
}

/** The code to type on the phone, one key cap per digit, and how long it still holds. */
@Composable
private fun PairCode(attempt: PairAttempt, trouble: String, focus: Modifier, cancel: () -> Unit) {
    val started = remember(attempt) { android.os.SystemClock.elapsedRealtime() }
    var left by remember(attempt) { mutableIntStateOf(300) }
    LaunchedEffect(attempt) {
        while (left > 0) {
            left = (300 - (android.os.SystemClock.elapsedRealtime() - started) / 1000).toInt().coerceAtLeast(0)
            delay(250)
        }
    }
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(36.dp), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(14.dp)) {
            Text("在手机上输入配对码", color = Ink, fontFamily = androidx.compose.ui.text.font.FontFamily.SansSerif, fontSize = 32.sp, fontWeight = FontWeight.SemiBold)
            Row(horizontalArrangement = Arrangement.spacedBy(10.dp), verticalAlignment = Alignment.CenterVertically) {
                attempt.code.forEachIndexed { i, digit ->
                    if (i == 3) Box(Modifier.width(14.dp))
                    KeyCap(digit)
                }
            }
            Text("打开 TrackFerry 网页或手机，进入「更多 → 电视」，输入这 6 位数字并批准。", color = Muted, fontSize = 16.sp)
            Row(horizontalArrangement = Arrangement.spacedBy(10.dp), verticalAlignment = Alignment.CenterVertically) {
                Text(attempt.server.name, color = Teal, fontSize = 15.sp)
                Text(if (trouble.isNotBlank()) trouble else "等待批准…", color = if (trouble.isNotBlank()) Accent else Muted, fontSize = 14.sp)
            }
            TvButton("换一台服务器", focus, onClick = cancel)
        }
        Countdown(left, 300)
    }
}

@Composable
private fun KeyCap(digit: Char) {
    val shape = androidx.compose.foundation.shape.RoundedCornerShape(14.dp)
    Box(Modifier.size(width = 62.dp, height = 78.dp)
        .background(androidx.compose.ui.graphics.Brush.verticalGradient(listOf(Color(0xFF2B3D4A), Color(0xFF16222B))), shape)
        .border(1.dp, androidx.compose.ui.graphics.Brush.verticalGradient(listOf(Color(0x80D9F1F4), Color(0x1AFFFFFF), Color(0x55F0B49E))), shape)
        .padding(bottom = 4.dp), contentAlignment = Alignment.Center) {
        Text(digit.toString(), color = Ink, fontSize = 42.sp, fontWeight = FontWeight.SemiBold)
    }
}

@Composable
private fun Countdown(left: Int, total: Int) {
    Box(Modifier.size(132.dp), contentAlignment = Alignment.Center) {
        androidx.compose.foundation.Canvas(Modifier.fillMaxSize()) {
            val stroke = 6.dp.toPx()
            val inset = stroke / 2
            val arc = androidx.compose.ui.geometry.Size(size.width - stroke, size.height - stroke)
            val at = androidx.compose.ui.geometry.Offset(inset, inset)
            drawArc(Color.White.copy(alpha = .12f), 0f, 360f, false, at, arc, style = androidx.compose.ui.graphics.drawscope.Stroke(stroke))
            drawArc(androidx.compose.ui.graphics.Brush.sweepGradient(listOf(FishLow, FishHigh, FishLow)), -90f, 360f * left / total, false, at, arc,
                style = androidx.compose.ui.graphics.drawscope.Stroke(stroke, cap = androidx.compose.ui.graphics.StrokeCap.Round))
        }
        Column(horizontalAlignment = Alignment.CenterHorizontally) {
            Text("${left / 60}:${(left % 60).toString().padStart(2, '0')}", color = Ink, fontSize = 26.sp, fontWeight = FontWeight.SemiBold)
            Text("后失效", color = Muted, fontSize = 12.sp)
        }
    }
}

@Composable
internal fun TvInput(value: String, onValue: (String) -> Unit, hint: String, modifier: Modifier = Modifier, glyph: TvGlyph? = null,
                     inset: Boolean = true, submit: () -> Unit) {
    var focus by remember { mutableStateOf(false) }
    val keyboard = LocalSoftwareKeyboardController.current
    val shape = RoundedCornerShape(12.dp)
    val surface = if (inset) Modifier.background(Backdrop.copy(alpha = .78f), shape) else Modifier
    val edge = if (focus || inset) Modifier.border(1.dp, if (focus) StageGlass else androidx.compose.ui.graphics.SolidColor(Color(0xFF39535B)), shape) else Modifier
    BasicTextField(value, onValue, modifier.fillMaxWidth().onFocusChanged { focus = it.hasFocus }.then(surface).then(edge)
        .padding(horizontal = 12.dp, vertical = if (inset) 12.dp else 8.dp).semantics { contentDescription = hint },
        singleLine = true, textStyle = TextStyle(color = Ink, fontSize = 20.sp), cursorBrush = SolidColor(Accent),
        keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
        keyboardActions = KeyboardActions(onDone = { keyboard?.hide(); submit() }),
        decorationBox = { inner ->
            Row(horizontalArrangement = Arrangement.spacedBy(9.dp), verticalAlignment = Alignment.CenterVertically) {
                glyph?.let { StageIcon(it, Modifier.size(18.dp), if (focus) Accent else Muted) }
                Box(Modifier.weight(1f)) { if (value.isEmpty()) Text(hint, fontSize = 20.sp, color = Muted); inner() }
            }
        },
    )
}

internal fun Throwable.tvMessage(fallback: String): String = when (this) {
    is TvHttpError -> message ?: fallback
    is javax.net.ssl.SSLException -> "服务器身份校验失败"
    else -> fallback
}
