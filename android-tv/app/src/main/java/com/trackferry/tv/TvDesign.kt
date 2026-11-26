package com.trackferry.tv

import android.graphics.Bitmap
import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.StartOffset
import androidx.compose.animation.core.StartOffsetType
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.graphics.lerp
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.TextUnit
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.tv.material3.ClickableSurfaceDefaults
import androidx.tv.material3.Surface
import androidx.tv.material3.Text
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

internal val Ink = Color(0xFFF8F0E6)
internal val Muted = Color(0xFFB7BFBE)
internal val Accent = Color(0xFFFFC4A1)
internal val Teal = Color(0xFF9DD5D6)
internal val Backdrop = Color(0xFF10181D)
internal val Glass = Color.White.copy(alpha = .09f)

internal val CurrentTrackInk = Color(0xFFF4C95D)

@Composable
internal fun TrackTitle(title: String, current: Boolean, playing: Boolean, size: TextUnit, weight: FontWeight = FontWeight.Normal) {
    val state = if (current) Modifier.semantics { stateDescription = if (playing) "正在播放" else "当前曲目" } else Modifier
    Row(state, horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
        if (current && playing) TrackSoundBars()
        Text(title, color = if (current) CurrentTrackInk else Ink, fontSize = size, fontWeight = weight,
            maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

@Composable
private fun TrackSoundBars() {
    val animation = rememberInfiniteTransition(label = "current track")
    val first by animation.animateFloat(.2f, 1f, infiniteRepeatable(tween(1100), RepeatMode.Reverse), label = "first bar")
    val middle by animation.animateFloat(.2f, 1f, infiniteRepeatable(tween(780), RepeatMode.Reverse), label = "middle bar")
    val last by animation.animateFloat(.2f, 1f, infiniteRepeatable(tween(940), RepeatMode.Reverse, StartOffset(400, StartOffsetType.FastForward)), label = "last bar")
    Canvas(Modifier.size(width = 11.dp, height = 12.dp)) {
        val width = 2.5.dp.toPx()
        fun bar(x: Float, level: Float) {
            val height = size.height * level
            drawRect(CurrentTrackInk, Offset(x, size.height - height), Size(width, height))
        }
        val end = size.width - width
        bar(0f, first)
        bar(end / 2, middle)
        bar(end, last)
    }
}

private data class TidePalette(val sky: Color, val water: Color, val moon: Color)
private val palettes = listOf(
    TidePalette(Color(0xFF342B45), Color(0xFFBF745A), Color(0xFFFFD391)),
    TidePalette(Color(0xFF101F3C), Color(0xFF448781), Color(0xFFC1FAEE)),
    TidePalette(Color(0xFF25203F), Color(0xFF69619D), Color(0xFFEAD8FF)),
    TidePalette(Color(0xFF1A2C35), Color(0xFF76968B), Color(0xFFEFE7AF)),
    TidePalette(Color(0xFF28263A), Color(0xFFAC657D), Color(0xFFFFCCB9)),
    TidePalette(Color(0xFF1E2B46), Color(0xFF527EA4), Color(0xFFDEE9FC)),
)
private fun palette(seed: String) = palettes[(seed.hashCode() ushr 1) % palettes.size]
internal fun artTone(seed: String) = palette(seed).water
private val ArtIO = Dispatchers.IO.limitedParallelism(3)

@Composable
internal fun rememberArtwork(nas: NasConnection?, id: String, large: Boolean = false): Bitmap? {
    var cover by remember(nas, id, large) { mutableStateOf<Bitmap?>(null) }
    LaunchedEffect(nas, id, large) {
        if (nas == null || id.isBlank()) return@LaunchedEffect
        try { cover = withContext(ArtIO) { nas.artwork(id, large) } }
        catch (e: CancellationException) { throw e }
        catch (_: Exception) { /* The local cover is ready even when the network is not. */ }
    }
    return cover
}

internal fun coverTone(bitmap: Bitmap?, seed: String): Color {
    if (bitmap == null) return artTone(seed)
    var r = 0f
    var g = 0f
    var b = 0f
    var weight = 0f
    for (x in 1..7) for (y in 1..7) {
        val c = Color(bitmap.getPixel((bitmap.width - 1) * x / 8, (bitmap.height - 1) * y / 8))
        val w = c.alpha * (.15f + maxOf(c.red, c.green, c.blue) - minOf(c.red, c.green, c.blue))
        r += c.red * w
        g += c.green * w
        b += c.blue * w
        weight += w
    }
    return if (weight > 0) Color(r / weight, g / weight, b / weight) else artTone(seed)
}

@Composable
internal fun FerryBrand() {
    LivingBrand()
}

@Composable
internal fun TideBackdrop(tone: Color, playback: Boolean = false) {
    val wash by animateColorAsState(tone, tween(350), label = "cover tone")
    Canvas(Modifier.fillMaxSize()) {
        val horizon = size.height * if (playback) .69f else .48f
        drawRect(Brush.verticalGradient(listOf(lerp(Backdrop, wash, .24f), Backdrop, Color(0xFF0B1014))))
        val light = Offset(size.width * if (playback) .31f else .76f, horizon)
        drawRect(Brush.radialGradient(listOf(wash.copy(alpha = .42f), wash.copy(alpha = .10f), Color.Transparent), light, size.width * .6f))
        if (playback) {
            drawLine(wash.copy(alpha = .31f), Offset(0f, horizon), Offset(size.width, horizon), 1.dp.toPx())
            for (i in 1..8) {
                val y = horizon + size.height * (.012f * i * i / 4)
                val spread = size.width * (.012f + i * .009f)
                drawLine(wash.copy(alpha = .16f / i), Offset(light.x - spread, y), Offset(light.x + spread, y), 1.dp.toPx())
            }
        }
        drawRect(Brush.verticalGradient(listOf(Color.Transparent, Color(0xB5081014)), startY = size.height * .57f))
    }
}

/** Generative cover used only for catalog entries that have no artwork. */
@Composable
internal fun FerryCover(seed: String, bitmap: Bitmap?, modifier: Modifier = Modifier, radius: Int = 14) {
    val shape = RoundedCornerShape(radius.dp)
    Box(modifier.clip(shape).border(1.dp, Color.White.copy(alpha = .12f), shape)) {
        if (bitmap != null) {
            val image = remember(bitmap) { bitmap.asImageBitmap() }
            Image(image, contentDescription = null, modifier = Modifier.fillMaxSize(), contentScale = ContentScale.Crop)
        } else Canvas(Modifier.fillMaxSize()) {
            val p = palette(seed)
            val hash = seed.hashCode() ushr 1
            val moon = Offset(size.width * (.36f + hash % 21 * .014f), size.height * .35f)
            drawRect(Brush.verticalGradient(listOf(p.sky, lerp(p.sky, p.water, .74f), p.water)))
            drawCircle(p.moon.copy(alpha = .11f), size.minDimension * .285f, moon)
            drawCircle(p.moon.copy(alpha = .23f), size.minDimension * .19f, moon)
            drawCircle(p.moon, size.minDimension * .122f, moon)
            if (hash % 4 == 0) {
                for (i in 1..4) drawCircle(p.moon.copy(alpha = .32f / i), size.minDimension * (.15f + i * .055f), moon, style = Stroke(1.dp.toPx()))
            }
            val width = size.width
            val height = size.height
            val style = (hash / 23) % 5
            if (style == 1) {
                for (i in 0..10) {
                    val x = width * i / 10.5f
                    val top = height * (.65f + ((hash / 19 + i * 37) % 19) * .009f)
                    val block = width * (.07f + i % 3 * .012f)
                    drawRect(lerp(p.water, p.sky, .58f + (i % 4) * .1f), Offset(x, top), androidx.compose.ui.geometry.Size(block, height - top))
                    if (i % 3 != 1) drawCircle(p.moon.copy(alpha = .57f), 1.15.dp.toPx(), Offset(x + block * .64f, top + height * .074f))
                }
            } else {
                for (i in 0..3) {
                    val y = height * (.57f + i * .115f)
                    val rise = height * (.10f + (hash % 5) * .019f)
                    val wave = Path().apply {
                        moveTo(0f, y)
                        if (style == 3) {
                            lineTo(width * (.20f + i * .035f), y - rise)
                            lineTo(width * .42f, y + rise * .22f)
                            lineTo(width * .68f, y - rise * .76f)
                            lineTo(width, y + rise * .2f)
                        } else {
                            cubicTo(width * .35f, y - rise, width * .63f, y + (if (i % 2 == 0) rise else -rise), width, y - rise * .35f)
                        }
                        lineTo(width, height)
                        lineTo(0f, height)
                        close()
                    }
                    drawPath(wave, lerp(p.water, p.sky, .37f + i * .15f))
                }
            }
        }
    }
}

@Composable
internal fun FerryTile(modifier: Modifier = Modifier, onClick: () -> Unit, enabled: Boolean = true, content: @Composable () -> Unit) {
    var focus by remember { mutableStateOf(false) }
    val outline by animateColorAsState(if (focus) Accent else Color.White.copy(alpha = .12f), tween(160), label = "focus outline")
    val shape = RoundedCornerShape(13.dp)
    Surface(
        onClick = onClick, enabled = enabled,
        modifier = modifier.onFocusChanged { focus = it.isFocused }
            .border(2.dp, outline, shape),
        shape = ClickableSurfaceDefaults.shape(shape),
        colors = ClickableSurfaceDefaults.colors(containerColor = Glass, focusedContainerColor = Color(0xFF283137), contentColor = Ink, focusedContentColor = Ink),
        scale = ClickableSurfaceDefaults.scale(focusedScale = 1f, pressedScale = 1f),
    ) { content() }
}
