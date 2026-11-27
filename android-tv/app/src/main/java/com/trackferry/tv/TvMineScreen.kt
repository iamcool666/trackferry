package com.trackferry.tv

import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxScope
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import android.widget.Toast
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.ui.text.font.FontFamily
import kotlinx.coroutines.delay
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.tv.material3.Text

@Composable
internal fun StagePanel(modifier: Modifier = Modifier, padding: Dp = 14.dp, content: @Composable BoxScope.() -> Unit) {
    val shape = RoundedCornerShape(19.dp)
    Box(modifier.clip(shape).background(Brush.verticalGradient(listOf(Color(0x29E3F5FA), Color(0x25233642), Color(0x45283949))))
        .border(1.dp, Brush.verticalGradient(listOf(Color(0x60BDEDF4), Color(0x247CACC3), Color(0x37DDA18D))), shape)
        .padding(padding), content = content)
}

@Composable
internal fun TvMineScreen(nas: NasConnection, shelf: Shelf?, busy: Boolean, gutter: Dp, short: Boolean,
                          open: (Collection) -> Unit, settings: () -> Unit) {
    val favorites = shelf?.collections?.firstOrNull { it.source == "favorites" }
    val server = nas.link.server
    val context = LocalContext.current
    val version = remember(context) { TvUpdate.installed(context) }
    val gap = if (short) 9.dp else 14.dp
    val first = remember { FocusRequester() }
    LaunchedEffect(Unit) { withFrameNanos {}; runCatching { first.requestFocus() } }
    Row(Modifier.fillMaxSize().padding(horizontal = gutter - 8.dp), horizontalArrangement = Arrangement.spacedBy(if (short) 8.dp else 14.dp)) {
        Column(Modifier.weight(1.1f).fillMaxHeight().verticalScroll(rememberScrollState()).padding(horizontal = 8.dp).padding(top = 4.dp, bottom = 36.dp), verticalArrangement = Arrangement.spacedBy(gap)) {
            Text("设置", color = Ink, fontFamily = FontFamily.SansSerif, fontSize = if (short) 26.sp else 34.sp, fontWeight = FontWeight.SemiBold)
            StagePanel(Modifier.fillMaxWidth(), padding = 14.dp) {
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(14.dp), verticalAlignment = Alignment.CenterVertically) {
                    Image(painterResource(R.drawable.fish_mark), null, Modifier.size(if (short) 46.dp else 58.dp))
                    Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
                        Text(server.name, color = Ink, fontSize = if (short) 17.sp else 21.sp, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        Text("已配对 · ${server.address.host}", color = Teal, fontSize = 13.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    }
                    TvButton("连接设置", Modifier.focusRequester(first), onClick = settings)
                }
            }
            SoundPanel(short)
            UpdatePanel(nas, version, short)
        }
        Column(Modifier.weight(.9f).fillMaxHeight().verticalScroll(rememberScrollState()).padding(horizontal = 8.dp).padding(top = 4.dp, bottom = 36.dp), verticalArrangement = Arrangement.spacedBy(gap)) {
            Box(Modifier.height(if (short) 30.dp else 40.dp))
            StageTile(Modifier.fillMaxWidth().height(if (short) 64.dp else 80.dp), enabled = favorites != null, onClick = { favorites?.let(open) }) {
                Box(Modifier.fillMaxSize()) {
                    FavoriteBackdrop(Modifier.fillMaxSize())
                    Row(Modifier.fillMaxSize().padding(horizontal = 12.dp), horizontalArrangement = Arrangement.spacedBy(10.dp), verticalAlignment = Alignment.CenterVertically) {
                        FavoriteHeart(Modifier.size(if (short) 44.dp else 54.dp))
                        Text("我喜欢的音乐", Modifier.weight(1f), color = Ink, fontSize = if (short) 16.sp else 19.sp, fontWeight = FontWeight.SemiBold)
                        Text(favorites?.let { "${it.count} 首" } ?: if (busy) "读取中" else "", color = Ink.copy(alpha = .76f), fontSize = 13.sp)
                    }
                }
            }
            StagePanel(Modifier.fillMaxWidth(), padding = 16.dp) {
                Column(verticalArrangement = Arrangement.spacedBy(7.dp)) {
                    Row(horizontalArrangement = Arrangement.spacedBy(10.dp), verticalAlignment = Alignment.CenterVertically) {
                        StageIcon(TvGlyph.About, Modifier.size(18.dp), Teal)
                        Text("关于", color = Ink, fontSize = 18.sp, fontWeight = FontWeight.SemiBold)
                    }
                    Text("TrackFerry TV $version · ${TvUpdate.abi}", color = Ink, fontSize = 15.sp)
                    Text("作者 madrays · MIT 许可开源", color = Muted, fontSize = 14.sp)
                    Text("github.com/madrays/trackferry", color = Teal, fontSize = 14.sp)
                    Text("把曲库（飞牛音乐或音乐文件夹）和在线音源送到电视、手机和小爱音箱上。", color = Muted, fontSize = 13.sp)
                }
            }
        }
    }
}

@Composable
internal fun SoundPanel(short: Boolean, modifier: Modifier = Modifier) {
    val context = LocalContext.current
    val chosen by SoundSettings.preset.collectAsState()
    StagePanel(modifier.fillMaxWidth(), padding = 14.dp) {
        Column(verticalArrangement = Arrangement.spacedBy(9.dp)) {
            Row(horizontalArrangement = Arrangement.spacedBy(10.dp), verticalAlignment = Alignment.CenterVertically) {
                StageIcon(TvGlyph.Volume, Modifier.size(18.dp), Teal)
                Text("音效", color = Ink, fontSize = 18.sp, fontWeight = FontWeight.SemiBold)
                Text("和网页里的预设一样，只作用于这台电视", color = Muted, fontSize = 12.sp)
            }
            EQ_PRESETS.chunked(4).forEach { row ->
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    row.forEach { p -> PresetChip(p.label, p.name == chosen, Modifier.weight(1f)) { SoundSettings.choose(context, p.name) } }
                }
            }
            if (!short) Text("小爱音箱自己解码播放，收不到这里的音效。", color = Muted, fontSize = 12.sp)
        }
    }
}

/** One preset: the one in use is lit teal with a dot, plain to see from the sofa; focus is the warm fill. */
@Composable
private fun PresetChip(label: String, chosen: Boolean, modifier: Modifier, onClick: () -> Unit) {
    var focused by remember { mutableStateOf(false) }
    val shape = RoundedCornerShape(22.dp)
    androidx.tv.material3.Surface(onClick = onClick, modifier = modifier.height(44.dp).onFocusChanged { focused = it.isFocused }
        .then(if (chosen && !focused) Modifier.border(1.5.dp, FishLow, shape) else Modifier),
        shape = androidx.tv.material3.ClickableSurfaceDefaults.shape(shape),
        colors = androidx.tv.material3.ClickableSurfaceDefaults.colors(
            containerColor = if (chosen) FishLow.copy(alpha = .26f) else Glass, contentColor = if (chosen) Color(0xFFD9F3FA) else Ink,
            focusedContainerColor = Accent, focusedContentColor = Backdrop),
        scale = androidx.tv.material3.ClickableSurfaceDefaults.scale(focusedScale = 1.05f, pressedScale = .98f)) {
        Row(Modifier.fillMaxSize(), horizontalArrangement = Arrangement.spacedBy(7.dp, Alignment.CenterHorizontally), verticalAlignment = Alignment.CenterVertically) {
            if (chosen) Box(Modifier.size(7.dp).clip(CircleShape).background(if (focused) Backdrop else FishLow))
            Text(label, fontSize = 15.sp, fontWeight = if (chosen) FontWeight.Bold else FontWeight.Medium, maxLines = 1)
        }
    }
}

@Composable
private fun UpdatePanel(nas: NasConnection, version: String, short: Boolean) {
    val context = LocalContext.current
    val step by TvUpdate.step.collectAsState()
    LaunchedEffect(nas) {
        val now = TvUpdate.step.value
        if (now is TvUpdate.Step.Idle || now is TvUpdate.Step.Current || now is TvUpdate.Step.Failed) TvUpdate.launch { TvUpdate.check(context, nas) }
    }
    // Back from the system screen that allows installing: carry on by itself.
    LaunchedEffect(step is TvUpdate.Step.Permission) {
        while (TvUpdate.step.value is TvUpdate.Step.Permission) { delay(1_000); TvUpdate.launch { TvUpdate.resume(context, nas) } }
    }
    StagePanel(Modifier.fillMaxWidth(), padding = 14.dp) {
        Column(verticalArrangement = Arrangement.spacedBy(9.dp)) {
            Row(horizontalArrangement = Arrangement.spacedBy(10.dp), verticalAlignment = Alignment.CenterVertically) {
                StageIcon(TvGlyph.Settings, Modifier.size(18.dp), Teal)
                Text("应用更新", color = Ink, fontSize = 18.sp, fontWeight = FontWeight.SemiBold)
                Text("当前 $version", color = Muted, fontSize = 12.sp)
            }
            fun pct(done: Long, total: Long) = if (total > 0) " ${(done * 100 / total).coerceIn(0, 100)}%" else ""
            val (line, fraction) = when (val s = step) {
                TvUpdate.Step.Idle, TvUpdate.Step.Checking -> "正在问服务器有没有新版…" to null
                is TvUpdate.Step.Current -> (if (s.version == version) "已是最新版" else "已是最新版（服务器 ${s.version}）") to null
                is TvUpdate.Step.Available -> "服务器已经是 ${s.version}，这台电视可以跟上${if (s.size > 0) "（${s.size / 1_048_576 + 1} MB）" else ""}" to null
                is TvUpdate.Step.Preparing -> "服务器正在取 ${s.version} 并核对签名${pct(s.done, s.total)}" to s.total.takeIf { it > 0 }?.let { s.done.toFloat() / it }
                is TvUpdate.Step.Downloading -> "正在下载 ${s.version}${pct(s.done, s.total)}" to s.total.takeIf { it > 0 }?.let { s.done.toFloat() / it }
                is TvUpdate.Step.Installing -> "已交给系统安装，按屏幕上的提示确认" to 1f
                is TvUpdate.Step.Permission -> "要先允许 TrackFerry 安装应用，允许后会自己接着装" to null
                is TvUpdate.Step.Failed -> s.message to null
            }
            Text(line, color = if (step is TvUpdate.Step.Failed) Accent else Ink.copy(alpha = .86f), fontSize = 14.sp)
            if (fraction != null) {
                val shown = remember { mutableLongStateOf(0) }
                shown.longValue = (fraction.coerceIn(0f, 1f) * 1000).toLong()
                FishTrack(shown, 1000, Modifier.fillMaxWidth(), fish = 22.dp)
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                when (val s = step) {
                    is TvUpdate.Step.Available -> TvButton("更新到 ${s.version}", primary = true) { TvUpdate.launch { TvUpdate.install(context, nas) } }
                    is TvUpdate.Step.Permission -> TvButton("去允许", primary = true) {
                        if (!TvUpdate.askPermission(context)) Toast.makeText(context, "请在系统设置的“安装未知应用”里允许 TrackFerry", Toast.LENGTH_LONG).show()
                    }
                    is TvUpdate.Step.Failed -> TvButton("重试", primary = true) {
                        TvUpdate.launch { if (s.version.isNotEmpty()) TvUpdate.install(context, nas) else TvUpdate.check(context, nas) }
                    }
                    is TvUpdate.Step.Current -> TvButton("再检查一次") { TvUpdate.launch { TvUpdate.check(context, nas) } }
                    else -> {}
                }
            }
            if (!short) Text("新版由服务器从发布页取来并核对签名，电视再核对一次后才安装。", color = Muted, fontSize = 12.sp)
        }
    }
}
