package com.trackferry.tv

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.CubicBezierEasing
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.Image
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.MutableFloatState
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.composed
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.BlendMode
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.CompositingStrategy
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.tv.material3.Text
import kotlin.math.PI
import kotlin.math.cos
import kotlin.math.sin

/** The easing used for everything that arrives: quick to start, long and soft to settle. */
internal val Settle = CubicBezierEasing(.16f, 1f, .3f, 1f)

/** Seconds since this was first shown, ticking every frame while [running]; still otherwise. */
@Composable
internal fun rememberClock(running: Boolean): MutableFloatState {
    val t = remember { mutableFloatStateOf(0f) }
    val calm by Motion.calm.collectAsState()
    LaunchedEffect(running, calm) {
        if (!running || calm) return@LaunchedEffect
        var last = 0L
        while (true) withFrameNanos { now ->
            if (last != 0L) t.floatValue += minOf(now - last, 50_000_000L) / 1e9f
            last = now
        }
    }
    return t
}

/**
 * The fish of the brand, swimming: a slow rise and fall with the body tilting along, and now and
 * then a glint of light along the name. Only layer properties change, so it costs one small redraw.
 */
@Composable
internal fun LivingBrand(mark: Int = 39, text: Int = 21) {
    val t = rememberClock(true)
    Row(horizontalArrangement = Arrangement.spacedBy(7.dp), verticalAlignment = Alignment.CenterVertically) {
        Image(painterResource(R.drawable.fish_mark), contentDescription = null, modifier = Modifier.size(mark.dp).graphicsLayer {
            val s = t.floatValue
            translationY = sin(s * 2f * PI.toFloat() / 3.6f) * 1.6.dp.toPx()
            translationX = sin(s * 2f * PI.toFloat() / 7.2f) * 1.2.dp.toPx()
            rotationZ = cos(s * 2f * PI.toFloat() / 3.6f) * 4f
        })
        Text("TrackFerry", Modifier.graphicsLayer { compositingStrategy = CompositingStrategy.Offscreen }.drawWithContent {
            drawContent()
            // A glint every nine seconds, a little over half a second long.
            val phase = (t.floatValue % 9f) / .7f
            if (phase in 0f..1f) {
                val x = -size.width * .4f + size.width * 1.8f * phase
                drawRect(Brush.linearGradient(listOf(Color.Transparent, Color.White.copy(alpha = .85f), Color.Transparent),
                    Offset(x - size.width * .22f, 0f), Offset(x + size.width * .22f, size.height)), blendMode = BlendMode.SrcAtop)
            }
        }, color = Ink, fontSize = text.sp, fontWeight = FontWeight.SemiBold, letterSpacing = (-.55).sp)
    }
}

/**
 * A band of light that crosses a card once when it takes the focus: the moment of choosing,
 * felt rather than read. Draws only during the sweep.
 */
internal fun Modifier.sheen(focused: Boolean, radius: Float = 0f): Modifier = composed {
    val sweep = remember { Animatable(1f) }
    LaunchedEffect(focused) {
        if (focused) { sweep.snapTo(0f); sweep.animateTo(1f, tween(950, easing = Settle)) }
    }
    drawWithContent {
        drawContent()
        val p = sweep.value
        if (p < 1f) {
            val w = size.width
            val x = -w * .5f + (w * 2f) * p
            val fade = if (p > .7f) (1f - p) / .3f else 1f
            drawRect(Brush.linearGradient(listOf(Color.Transparent, Color.White.copy(alpha = .16f * fade), Color.Transparent),
                Offset(x - w * .28f, 0f), Offset(x + w * .08f, size.height)))
        }
    }
}

/**
 * Arrival in order: each part rises a little and fades in, [order] steps after the first, all
 * driven by one shared progress so scrolling back never plays it again.
 */
internal fun Modifier.rise(progress: Animatable<Float, *>, order: Int): Modifier = graphicsLayer {
    val p = ((progress.value - order * .09f) / .55f).coerceIn(0f, 1f)
    val e = Settle.transform(p)
    alpha = e
    translationY = (1f - e) * 28.dp.toPx()
}

/** A slow breath for a cover while music plays; it settles back to rest otherwise. */
@Composable
internal fun rememberBreath(playing: Boolean): MutableFloatState {
    val breath = remember { mutableFloatStateOf(1f) }
    val calm by Motion.calm.collectAsState()
    LaunchedEffect(playing, calm) {
        val from = breath.floatValue
        if (!playing || calm) {
            androidx.compose.animation.core.animate(from, 1f, animationSpec = tween(600)) { v, _ -> breath.floatValue = v }
            return@LaunchedEffect
        }
        val start = withFrameNanos { it }
        while (true) withFrameNanos { now ->
            val t = (now - start) / 1e9f
            val wave = 1f + .009f * (1f - cos(t * 2f * PI.toFloat() / 6.2f))
            breath.floatValue = if (t < 1f) from + (wave - from) * t else wave
        }
    }
    return breath
}

/**
 * The opening: out of the dark a light gathers, the fish surfaces and a band of light crosses it,
 * a line of light runs out to both sides, the name rises, and it all dissolves into the app,
 * which has been loading underneath the whole time. About 1.8 seconds, once per launch.
 */
@Composable
internal fun Intro(done: () -> Unit) {
    val p = remember { Animatable(0f) }
    val out = remember { Animatable(0f) }
    LaunchedEffect(Unit) {
        p.animateTo(1f, tween(1350, easing = androidx.compose.animation.core.LinearEasing))
        out.animateTo(1f, tween(520, easing = Settle))
        done()
    }
    fun seg(a: Float, b: Float) = Settle.transform(((p.value - a) / (b - a)).coerceIn(0f, 1f))
    Box(Modifier.fillMaxSize().graphicsLayer {
        alpha = 1f - out.value
        val s = 1f + .05f * out.value
        scaleX = s; scaleY = s
    }, contentAlignment = Alignment.Center) {
        Canvas(Modifier.fillMaxSize()) {
            drawRect(Backdrop)
            val g = seg(0f, .6f)
            drawRect(Brush.radialGradient(listOf(Teal.copy(alpha = .22f * g), Color.Transparent), Offset(size.width * .46f, size.height * .46f), size.width * (.25f + .2f * g)))
            drawRect(Brush.radialGradient(listOf(Accent.copy(alpha = .16f * g), Color.Transparent), Offset(size.width * .56f, size.height * .54f), size.width * (.18f + .16f * g)))
        }
        Column(horizontalAlignment = Alignment.CenterHorizontally) {
            Box(Modifier.size(112.dp).graphicsLayer {
                val e = seg(.08f, .62f)
                alpha = e
                val s = .82f + .18f * e
                scaleX = s; scaleY = s
                translationY = (1f - e) * 26.dp.toPx()
                rotationZ = (1f - e) * -8f
                compositingStrategy = CompositingStrategy.Offscreen
            }.drawWithContent {
                drawContent()
                val sweep = ((p.value - .42f) / .4f).coerceIn(0f, 1f)
                if (sweep in .001f..0.999f) {
                    val x = -size.width * .5f + size.width * 2f * sweep
                    drawRect(Brush.linearGradient(listOf(Color.Transparent, Color.White.copy(alpha = .9f), Color.Transparent),
                        Offset(x - size.width * .25f, 0f), Offset(x + size.width * .1f, size.height)), blendMode = BlendMode.SrcAtop)
                }
            }) { Image(painterResource(R.drawable.fish_mark), null, Modifier.fillMaxSize()) }
            Box(Modifier.height(28.dp))
            Text("TrackFerry", Modifier.graphicsLayer {
                val e = seg(.45f, 1f)
                alpha = e
                translationY = (1f - e) * 14.dp.toPx()
            }, color = Ink, fontSize = 34.sp, fontWeight = FontWeight.SemiBold, letterSpacing = 1.sp)
            // The line of light runs out from the middle, between the name and the line under it.
            Canvas(Modifier.padding(top = 14.dp).width(560.dp).height(2.dp)) {
                val half = size.width / 2 * seg(.35f, .95f)
                if (half > 0f) drawRect(Brush.horizontalGradient(listOf(Color.Transparent, Teal.copy(alpha = .8f), Accent, Teal.copy(alpha = .8f), Color.Transparent),
                    startX = size.width / 2 - half, endX = size.width / 2 + half),
                    Offset(size.width / 2 - half, 0f), androidx.compose.ui.geometry.Size(half * 2, 1.2.dp.toPx()))
            }
            Text("把家里的音乐，送到每一处", Modifier.padding(top = 12.dp).graphicsLayer { alpha = seg(.62f, 1f) * .7f },
                color = Ink, fontSize = 15.sp, letterSpacing = 3.sp)
            Box(Modifier.width(1.dp))
        }
    }
}
