package com.trackferry.tv

import android.graphics.Bitmap
import androidx.compose.animation.Crossfade
import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.ui.Alignment
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.sp
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.graphics.drawscope.DrawScope
import androidx.compose.ui.graphics.drawscope.rotate
import androidx.compose.ui.graphics.drawscope.scale
import androidx.compose.ui.graphics.lerp
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.IntSize
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlin.math.max
import kotlin.math.sin

/**
 * A blur done in software on a small copy of the cover, so every television gets the same soft
 * light (RenderEffect needs Android 12; many sets are older). Three box passes read as Gaussian.
 */
internal fun softened(src: Bitmap, side: Int = 64, radius: Int = 5): Bitmap {
    val small = Bitmap.createScaledBitmap(src, side, side, true)
    val px = IntArray(side * side)
    small.getPixels(px, 0, side, 0, 0, side, side)
    val tmp = IntArray(px.size)
    repeat(3) {
        box(px, tmp, side, radius, horizontal = true)
        box(tmp, px, side, radius, horizontal = false)
    }
    return Bitmap.createBitmap(px, side, side, Bitmap.Config.ARGB_8888)
}

private fun box(src: IntArray, dst: IntArray, side: Int, r: Int, horizontal: Boolean) {
    val span = r * 2 + 1
    for (line in 0 until side) {
        var a = 0; var red = 0; var g = 0; var b = 0
        fun at(i: Int): Int {
            val c = i.coerceIn(0, side - 1)
            return if (horizontal) src[line * side + c] else src[c * side + line]
        }
        for (i in -r..r) { val p = at(i); a += p ushr 24; red += (p shr 16) and 255; g += (p shr 8) and 255; b += p and 255 }
        for (i in 0 until side) {
            val out = ((a / span) shl 24) or ((red / span) shl 16) or ((g / span) shl 8) or (b / span)
            if (horizontal) dst[line * side + i] = out else dst[i * side + line] = out
            val gone = at(i - r); val come = at(i + r + 1)
            a += (come ushr 24) - (gone ushr 24)
            red += ((come shr 16) and 255) - ((gone shr 16) and 255)
            g += ((come shr 8) and 255) - ((gone shr 8) and 255)
            b += (come and 255) - (gone and 255)
        }
    }
}

/** The softened cover, worked out off the main thread. */
@Composable
internal fun rememberSoftened(cover: Bitmap?): ImageBitmap? {
    var soft by remember { mutableStateOf<Pair<Bitmap?, ImageBitmap?>>(null to null) }
    LaunchedEffect(cover) {
        if (cover == null) { soft = null to null; return@LaunchedEffect }
        val made = withContext(Dispatchers.Default) { runCatching { softened(cover).asImageBitmap() }.getOrNull() }
        soft = cover to made
    }
    return soft.second
}

/**
 * Keeps the motion from costing smoothness. The moving light and the breathing cover are the only
 * things that redraw every frame; when this television cannot keep up with them (frames slower
 * than about 36 a second for two seconds running) they stop for the rest of the run and the stage
 * stays still. Focus, lyrics and the spectrum keep moving: they are what the viewer acts on.
 */
internal object Motion {
    private const val WINDOW = 120
    private const val SLOW_NS = 28_000_000L
    private val state = kotlinx.coroutines.flow.MutableStateFlow(false)
    val calm: kotlinx.coroutines.flow.StateFlow<Boolean> = state
    private var frames = 0
    private var total = 0L

    /** One frame drawn [ns] after the one before it. */
    fun frame(ns: Long) {
        if (state.value) return
        if (ns > 250_000_000L) { frames = 0; total = 0; return } // hidden or paused, not slow
        frames++; total += ns
        if (frames < WINDOW) return
        if (total / frames > SLOW_NS) state.value = true
        frames = 0; total = 0
    }

    internal fun reset() { state.value = false; frames = 0; total = 0 }
}

/**
 * The stage behind the home page and the player: the cover of what plays, blurred into light that
 * drifts slowly while music plays and stands still when it stops; the low end of what is heard
 * lifts the glow a little. Darkened enough that white type always reads on it.
 */
@Composable
internal fun AmbientStage(cover: Bitmap?, tone: Color, playing: Boolean, modifier: Modifier = Modifier, focus: Offset = Offset(.30f, .40f)) {
    val soft = rememberSoftened(cover)
    val wash by animateColorAsState(tone, tween(900), label = "stage tone")
    var drift by remember { mutableFloatStateOf(0f) }
    var pulse by remember { mutableFloatStateOf(0f) }
    val calm by Motion.calm.collectAsState()
    LaunchedEffect(playing, calm) {
        if (calm) { pulse = 0f; return@LaunchedEffect }
        // Paused, the light still drifts, at a third of the pace; the bass only moves it while heard.
        val pace = if (playing) 1f else .35f
        val frame = FloatArray(Spectrum.BANDS)
        var last = 0L
        while (true) {
            withFrameNanos { now ->
                if (last != 0L) {
                    val step = now - last
                    Motion.frame(step)
                    // A long gap (the app was hidden) is not drift: the light carries on from where it was.
                    drift += pace * minOf(step, 50_000_000L) / 1e9f
                }
                last = now
            }
            val low = if (playing && Spectrum.now(frame)) (frame[0] + frame[1] + frame[2] + frame[3]) / 4f else 0f
            pulse += (low - pulse) * if (low > pulse) .35f else .06f
        }
    }
    Box(modifier.fillMaxSize().background(lerp(Backdrop, wash, .18f))) {
        Crossfade(soft, animationSpec = tween(1100), label = "stage cover") { image ->
            if (image != null) Canvas(Modifier.fillMaxSize()) { flow(image, drift) }
            else Image(painterResource(R.drawable.tv_evening), null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop, alpha = .85f)
        }
        Canvas(Modifier.fillMaxSize()) {
            val light = Offset(size.width * focus.x, size.height * focus.y)
            // The glow behind the cover, lifted by the bass.
            drawRect(Brush.radialGradient(listOf(wash.copy(alpha = .30f + .22f * pulse), Color.Transparent), light, size.width * (.42f + .05f * pulse)))
            // Keep type readable: darker towards the bottom and the right, a soft vignette all round.
            drawRect(Brush.verticalGradient(listOf(Color(0x59060B0F), Color(0x33060B0F), Color(0xC7060B0F))))
            drawRect(Brush.horizontalGradient(listOf(Color(0x40060B0F), Color.Transparent, Color(0x66060B0F))))
            drawRect(Brush.radialGradient(listOf(Color.Transparent, Color(0x9E05090C)), center, max(size.width, size.height) * .78f))
        }
    }
}

/** Two copies of the soft cover, large and turning slowly against each other. */
private fun DrawScope.flow(image: ImageBitmap, t: Float) {
    val side = max(size.width, size.height) * 1.55f
    fun layer(angle: Float, dx: Float, dy: Float, zoom: Float, alpha: Float) {
        val at = Offset(center.x + dx * size.width - side / 2, center.y + dy * size.height - side / 2)
        rotate(angle, center) {
            scale(zoom, center) {
                drawImage(image, IntOffset.Zero, IntSize(image.width, image.height),
                    IntOffset(at.x.toInt(), at.y.toInt()), IntSize(side.toInt(), side.toInt()), alpha = alpha)
            }
        }
    }
    layer(t * 2.4f, .06f * sin(t * .07f), .05f * sin(t * .05f + 1f), 1.0f + .06f * sin(t * .09f), 1f)
    layer(-t * 3.1f + 140f, -.10f + .07f * sin(t * .06f + 2f), .08f * sin(t * .08f), 1.15f, .55f)
}

/** A cover lifted off the stage: a coloured glow beneath it, a fine light rim. */
@Composable
internal fun GlowCover(seed: String, bitmap: Bitmap?, tone: Color, modifier: Modifier, radius: Int = 18, glow: Dp = 34.dp) {
    Box(modifier.drawBehind {
        val spread = glow.toPx()
        drawRoundRect(Brush.radialGradient(listOf(tone.copy(alpha = .55f), tone.copy(alpha = .18f), Color.Transparent),
            Offset(size.width / 2, size.height * .58f), max(size.width, size.height) * .5f + spread),
            Offset(-spread, -spread * .6f), Size(size.width + spread * 2, size.height + spread * 2.2f))
        // A soft shadow falling below, made of gradient so it has no edge.
        drawOval(Brush.radialGradient(listOf(Color.Black.copy(alpha = .28f), Color.Transparent),
            Offset(size.width / 2, size.height * .96f), size.width * .55f),
            Offset(-size.width * .05f, size.height * .72f), Size(size.width * 1.1f, size.height * .5f))
    }) {
        FerryCover(seed, bitmap, Modifier.fillMaxSize(), radius)
    }
}

/**
 * The big buttons of the stage: a cream pill for the main action, glass for the rest. Focus warms
 * the fill and lifts the pill a little; it never draws a frame around it.
 */
@Composable
internal fun PillButton(label: String, glyph: TvGlyph? = null, modifier: Modifier = Modifier, primary: Boolean = false,
                        enabled: Boolean = true, short: Boolean = false, onClick: () -> Unit) {
    val shape = androidx.compose.foundation.shape.RoundedCornerShape(50)
    var focused by remember { mutableStateOf(false) }
    androidx.tv.material3.Surface(onClick = onClick, enabled = enabled,
        modifier = modifier.height(if (short) 40.dp else 50.dp).onFocusChanged { focused = it.isFocused }
            .then(if (!primary && !focused) Modifier.border(1.dp, Color.White.copy(alpha = .16f), shape) else Modifier),
        shape = androidx.tv.material3.ClickableSurfaceDefaults.shape(shape),
        colors = androidx.tv.material3.ClickableSurfaceDefaults.colors(
            containerColor = if (primary) Ink else Color.White.copy(alpha = .10f), contentColor = if (primary) Backdrop else Ink,
            focusedContainerColor = Accent, focusedContentColor = Backdrop,
            disabledContainerColor = Color.White.copy(alpha = .06f), disabledContentColor = Ink.copy(alpha = .35f)),
        scale = androidx.tv.material3.ClickableSurfaceDefaults.scale(focusedScale = 1.07f, pressedScale = .97f)) {
        Row(Modifier.fillMaxHeight().padding(horizontal = if (short) 16.dp else 24.dp), horizontalArrangement = Arrangement.spacedBy(9.dp),
            verticalAlignment = Alignment.CenterVertically) {
            val ink = if (focused || primary) Backdrop else if (enabled) Ink else Ink.copy(alpha = .35f)
            glyph?.let { StageIcon(it, Modifier.size(if (short) 14.dp else 17.dp), ink) }
            androidx.tv.material3.Text(label, color = ink, fontSize = if (short) 14.sp else 17.sp, fontWeight = FontWeight.SemiBold, maxLines = 1)
        }
    }
}

/** A small warm light that breathes with the low end of what is heard; a still dot when nothing plays. */
@Composable
internal fun BassDot(modifier: Modifier, playing: Boolean) {
    val level = remember { mutableFloatStateOf(0f) }
    LaunchedEffect(playing) {
        val frame = FloatArray(Spectrum.BANDS)
        while (playing || level.floatValue > .01f) {
            withFrameNanos { }
            val low = if (playing && Spectrum.now(frame)) (frame[0] + frame[1] + frame[2]) / 3f else 0f
            level.floatValue += (low - level.floatValue) * if (low > level.floatValue) .4f else .08f
        }
        level.floatValue = 0f
    }
    Canvas(modifier) {
        val l = level.floatValue
        val r = size.minDimension / 2
        drawCircle(Brush.radialGradient(listOf(Accent.copy(alpha = .30f + .45f * l), Color.Transparent), center, r * (1.0f + .9f * l)), r * (1.0f + .9f * l))
        drawCircle(if (playing) Accent else Muted, r * .34f)
    }
}
