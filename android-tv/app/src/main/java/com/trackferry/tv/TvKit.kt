package com.trackferry.tv

import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.focusable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.height
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.LongState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.draw.drawWithCache
import androidx.compose.ui.graphics.drawscope.translate
import androidx.compose.ui.graphics.lerp
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.onKeyEvent
import androidx.compose.ui.input.key.type
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.semantics.ProgressBarRangeInfo
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.progressBarRangeInfo
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.setProgress
import androidx.compose.ui.text.TextMeasurer
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.drawText
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.rememberTextMeasurer
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.media3.common.Player
import androidx.tv.material3.Text
import kotlinx.coroutines.delay
import kotlin.math.max
import kotlin.math.min

/** The two ends of the 小鱼 bar, as in the browser (.swim). */
internal val FishLow = Color(0xFF8FC9DC)
internal val FishHigh = Color(0xFFFFC2A4)

/** What the player does, kept current; without the queue when it is not needed. */
@Composable
internal fun rememberPlayback(player: Player, withItems: Boolean = true): Playback {
    var playback by remember(player) { mutableStateOf(if (withItems) player.snapshot() else player.snapshot(emptyList())) }
    DisposableEffect(player) {
        val listener = object : Player.Listener {
            override fun onEvents(player: Player, events: Player.Events) {
                playback = when {
                    !withItems -> player.snapshot(emptyList())
                    events.contains(Player.EVENT_TIMELINE_CHANGED) -> player.snapshot()
                    else -> player.snapshot(playback.items)
                }
            }
        }
        player.addListener(listener)
        playback = if (withItems) player.snapshot() else player.snapshot(emptyList())
        onDispose { player.removeListener(listener) }
    }
    return playback
}

/** The position, read again every `every` ms while playing and on every change. */
@Composable
internal fun rememberPosition(player: Player, playback: Playback, every: Long): LongState {
    val position = remember(player) { mutableLongStateOf(player.currentPosition.coerceAtLeast(0)) }
    LaunchedEffect(player, playback) { position.longValue = player.currentPosition.coerceAtLeast(0) }
    LaunchedEffect(player, playback.playing) {
        while (playback.playing) {
            position.longValue = player.currentPosition.coerceAtLeast(0)
            delay(every)
        }
    }
    return position
}

/** The 小鱼 line without a hand on it: a thin track, the swum part, and the fish at its head. */
@Composable
internal fun FishTrack(position: LongState, duration: Long, modifier: Modifier, fish: Dp = 26.dp) {
    val painter = painterResource(R.drawable.fish_mark)
    Canvas(modifier.height(fish)) {
        val f = if (duration > 0) (position.longValue.toFloat() / duration).coerceIn(0f, 1f) else 0f
        val y = size.height / 2
        val h = 3.dp.toPx()
        drawRoundRect(Color.White.copy(alpha = .20f), Offset(0f, y - h / 2), Size(size.width, h), CornerRadius(h))
        drawRoundRect(Brush.horizontalGradient(listOf(FishLow, FishHigh), 0f, size.width), Offset(0f, y - h / 2), Size(size.width * f, h), CornerRadius(h))
        val side = fish.toPx()
        translate(size.width * f - side * .72f, y - side / 2) { with(painter) { draw(Size(side, side)) } }
    }
}

/**
 * The 小鱼 bar to seek with the remote: left and right move by 5 s, longer when held. Focus shows
 * as a thicker line, a bigger fish and the time above it, never as a frame.
 */
@Composable
internal fun FishSeekBar(
    position: LongState, duration: Long, seekable: Boolean, modifier: Modifier,
    currentPosition: () -> Long, seek: (Long) -> Unit,
) {
    var focused by remember { mutableStateOf(false) }
    var aim by remember { mutableStateOf<Long?>(null) }
    LaunchedEffect(aim) { if (aim != null) { delay(900); aim = null } }
    val grow by animateFloatAsState(if (focused) 1f else 0f, tween(220), label = "fish focus")
    val painter = painterResource(R.drawable.fish_mark)
    val measurer = rememberTextMeasurer()
    val fraction = if (duration > 0) (position.longValue.toFloat() / duration).coerceIn(0f, 1f) else 0f
    Box(modifier.height(64.dp).onFocusChanged { focused = it.isFocused }
        .onKeyEvent {
            if (it.type != KeyEventType.KeyDown || !seekable) return@onKeyEvent false
            val held = it.nativeKeyEvent.repeatCount
            val step = if (held > 12) 20_000L else if (held > 4) 10_000L else 5_000L
            val by = when (it.key) {
                Key.DirectionLeft -> -step
                Key.DirectionRight -> step
                else -> return@onKeyEvent false
            }
            val target = ((aim ?: currentPosition()) + by).coerceIn(0, max(0, duration - 500))
            aim = target
            seek(target)
            true
        }
        .semantics {
            contentDescription = "播放进度"
            progressBarRangeInfo = ProgressBarRangeInfo(fraction, 0f..1f)
            if (seekable) setProgress { seek((it * duration).toLong()); true }
        }
        .focusable(seekable)) {
        Canvas(Modifier.matchParentSize()) {
            val at = aim ?: position.longValue
            val f = if (duration > 0) (at.toFloat() / duration).coerceIn(0f, 1f) else 0f
            val y = size.height * .62f
            val h = (3 + 3 * grow).dp.toPx()
            drawRoundRect(Color.White.copy(alpha = .20f + .08f * grow), Offset(0f, y - h / 2), Size(size.width, h), CornerRadius(h))
            drawRoundRect(Brush.horizontalGradient(listOf(FishLow, FishHigh), 0f, size.width), Offset(0f, y - h / 2), Size(size.width * f, h), CornerRadius(h))
            val side = (30 + 12 * grow).dp.toPx()
            val head = size.width * f
            if (grow > 0f) drawCircle(FishHigh.copy(alpha = .22f * grow), side * .62f, Offset(head, y))
            translate(head - side * .72f, y - side / 2) { with(painter) { draw(Size(side, side)) } }
            if (grow > .05f) bubble(measurer, "${clock(at)} / ${clock(duration)}", head, y - side * .62f, grow)
        }
    }
}

private fun androidx.compose.ui.graphics.drawscope.DrawScope.bubble(measurer: TextMeasurer, text: String, x: Float, bottom: Float, alpha: Float) {
    val layout = measurer.measure(text, TextStyle(color = Ink, fontSize = 13.sp, fontWeight = FontWeight.Medium))
    val padX = 10.dp.toPx()
    val padY = 4.dp.toPx()
    val w = layout.size.width + padX * 2
    val h = layout.size.height + padY * 2
    val left = (x - w / 2).coerceIn(0f, size.width - w)
    val top = bottom - h
    drawRoundRect(Color(0xEE14202A).copy(alpha = .93f * alpha), Offset(left, top), Size(w, h), CornerRadius(h / 2))
    drawText(layout, topLeft = Offset(left + padX, top + padY), alpha = alpha)
}

/** Folds the analysed bands into fewer bars, by their loudest band. */
internal fun bandOf(frame: FloatArray, i: Int, bars: Int): Float {
    val from = i * frame.size / bars
    val to = max(from + 1, (i + 1) * frame.size / bars)
    var peak = 0f
    for (b in from until to) peak = max(peak, frame[b])
    return peak
}

@Composable
internal fun StageClock(size: Int = 14) {
    fun now() = android.text.format.DateFormat.format("HH:mm", System.currentTimeMillis()).toString()
    var time by remember { mutableStateOf(now()) }
    LaunchedEffect(Unit) { while (true) { time = now(); delay(5_000) } }
    Text(time, color = Ink.copy(alpha = .84f), fontSize = size.sp)
}

/**
 * The spectrum being heard as one light: a smooth curve, lowest notes in the middle and the highs
 * spreading out to both sides, filled with a glow that fades to nothing, with a bright edge and a
 * faint reflection below. Real levels only (see Spectrum); flat and quiet when nothing plays.
 * Paths and gradients are made once per size; a frame only redraws, it never recomposes.
 */
@Composable
internal fun SpectrumWave(modifier: Modifier, playing: Boolean, tone: Color = FishHigh, points: Int = 28) {
    val shown = remember(points) { FloatArray(points) }
    val frame = remember { FloatArray(Spectrum.BANDS) }
    // Each band against its own recent quiet and loud: a fixed scale leaves music sitting high and
    // nearly still, this shows how it moves. Slow to adapt, so a loud passage still reads loud.
    val floor = remember(points) { FloatArray(points) { 1f } }
    val ceil = remember(points) { FloatArray(points) }
    val smooth = remember(points) { FloatArray(points) }
    val tick = remember { mutableLongStateOf(0L) }
    LaunchedEffect(playing, points) {
        var rest = 0
        while (true) {
            withFrameNanos { tick.longValue = it }
            val heard = playing && Spectrum.now(frame)
            var moving = false
            for (i in 0 until points) {
                val raw = if (heard) bandOf(frame, i, points) else 0f
                if (heard) {
                    floor[i] = if (raw < floor[i]) raw else floor[i] + (raw - floor[i]) * .004f
                    ceil[i] = if (raw > ceil[i]) raw else ceil[i] - (ceil[i] - raw) * .006f
                }
                val span = max(.12f, ceil[i] - floor[i])
                val lift = ((raw - floor[i]) / span).coerceIn(0f, 1f)
                val target = if (heard) .15f * raw + .85f * lift * lift else 0f
                val now = shown[i]
                // Rise at once nearly, fall quickly too: the beat is seen, not smeared.
                val next = if (target > now) now + (target - now) * .7f else max(target, now - .045f)
                if (kotlin.math.abs(next - now) > .001f) moving = true
                shown[i] = next
            }
            if (!playing && !moving && ++rest > 3) break
        }
    }
    val low = lerp(FishLow, tone, .25f)
    Box(modifier.drawWithCache {
        val top = Path(); val fill = Path(); val reflect = Path()
        val base = size.height * .72f
        val fillBrush = Brush.verticalGradient(listOf(tone.copy(alpha = .55f), low.copy(alpha = .18f), Color.Transparent), 0f, base)
        val edgeBrush = Brush.horizontalGradient(listOf(Color.Transparent, low, tone, Color(0xFFFFE6DA), tone, low, Color.Transparent))
        val reflectBrush = Brush.verticalGradient(listOf(tone.copy(alpha = .16f), Color.Transparent), base, size.height)
        val xs = FloatArray(points * 2)
        val ys = FloatArray(points * 2)
        onDrawBehind {
            tick.longValue // redraw with every frame read above
            val n = points * 2
            // Mirror: the middle is the lowest band, both edges the highest.
            // Neighbouring bands are blended so the shape is hills, never plateaus; the loudness of
            // the whole sets a ripple running through it, so even a sparse sound moves as one body.
            var energy = 0f
            for (i in 0 until points) {
                val l = shown[max(0, i - 1)]; val r = shown[min(points - 1, i + 1)]
                val l2 = shown[max(0, i - 2)]; val r2 = shown[min(points - 1, i + 2)]
                smooth[i] = (l2 + 3 * l + 4 * shown[i] + 3 * r + r2) / 12f
                energy += shown[i]
            }
            energy /= points
            val t = tick.longValue / 1e9f
            for (k in 0 until n) {
                val band = if (k < points) points - 1 - k else k - points
                xs[k] = size.width * k / (n - 1f)
                // The edges rest on the line so the shape ends softly.
                val edge = kotlin.math.sin(Math.PI * k / (n - 1)).toFloat()
                val ripple = energy * (.5f + .5f * kotlin.math.sin(k * .42f - t * 3.1f)) * (.6f + .4f * kotlin.math.sin(k * .17f + t * 1.3f))
                ys[k] = base - base * .94f * min(1f, smooth[band] * .85f + ripple * .55f) * (.25f + .75f * edge)
            }
            top.reset()
            top.moveTo(xs[0], ys[0])
            for (k in 1 until n) {
                val mx = (xs[k - 1] + xs[k]) / 2
                top.cubicTo(mx, ys[k - 1], mx, ys[k], xs[k], ys[k])
            }
            fill.reset(); fill.addPath(top); fill.lineTo(size.width, base); fill.lineTo(0f, base); fill.close()
            reflect.reset()
            reflect.moveTo(xs[0], base)
            for (k in 1 until n) {
                val mx = (xs[k - 1] + xs[k]) / 2
                val ry0 = base + (base - ys[k - 1]) * .35f
                val ry1 = base + (base - ys[k]) * .35f
                reflect.cubicTo(mx, ry0, mx, ry1, xs[k], ry1)
            }
            reflect.lineTo(size.width, base); reflect.close()
            drawPath(reflect, reflectBrush)
            drawPath(fill, fillBrush)
            drawPath(top, edgeBrush, alpha = .35f, style = Stroke(6.dp.toPx(), cap = StrokeCap.Round))
            drawPath(top, edgeBrush, style = Stroke(1.6.dp.toPx(), cap = StrokeCap.Round))
            drawLine(Brush.horizontalGradient(listOf(Color.Transparent, Color.White.copy(alpha = .22f), Color.Transparent)),
                Offset(0f, base), Offset(size.width, base), 1f)
        }
    })
}
