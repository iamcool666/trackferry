package com.trackferry.tv

import android.graphics.Bitmap
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LongState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.graphics.lerp
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.tv.material3.ClickableSurfaceDefaults
import androidx.tv.material3.Surface
import kotlin.math.PI
import kotlin.math.exp
import kotlin.math.sin

internal val StageGlass = Brush.linearGradient(listOf(Color(0xFFBDEDF4), Color(0x66A2C1CA), Color(0xFFE8A994)))

internal enum class TvGlyph { Back, Play, Pause, Previous, Next, Shuffle, Repeat, RepeatOne, Volume, Queue, Favorite, Home, Discover, Search, Profile, Add, Lyrics, Lock, Settings, About, Close }

@Composable
internal fun GlyphButton(label: String, glyph: TvGlyph, modifier: Modifier = Modifier, size: Dp = 38.dp,
                         glass: Boolean = false, selected: Boolean = false, enabled: Boolean = true, onClick: () -> Unit) {
    var focused by remember { mutableStateOf(false) }
    Surface(onClick = onClick, enabled = enabled, modifier = modifier.size(size).onFocusChanged { focused = it.isFocused }
        .semantics { contentDescription = label }.then(if (focused || glass) Modifier.border(if (focused) 1.8.dp else 1.dp, StageGlass, CircleShape) else Modifier),
        shape = ClickableSurfaceDefaults.shape(CircleShape),
        colors = ClickableSurfaceDefaults.colors(containerColor = Color.Transparent, focusedContainerColor = Color.White.copy(alpha = .16f)),
        scale = ClickableSurfaceDefaults.scale(focusedScale = 1f, pressedScale = 1f)) {
        Box(Modifier.fillMaxSize().clip(CircleShape).then(if (glass) Modifier.background(Brush.verticalGradient(listOf(Color(0x664996A1), Color(0x58252F39), Color(0x88E6AC98)))) else Modifier), contentAlignment = Alignment.Center) {
            StageIcon(glyph, Modifier.size(if (glass) 24.dp else 20.dp), if (!enabled) Ink.copy(alpha = .28f) else if (selected) Accent else Ink)
        }
    }
}

@Composable
internal fun StageIcon(glyph: TvGlyph, modifier: Modifier = Modifier, color: Color = Ink) {
    Canvas(modifier) {
        val u = size.minDimension / 24f
        fun path(vararg points: Float, close: Boolean = false, filled: Boolean = false) {
            val p = Path().apply {
                moveTo(points[0] * u, points[1] * u)
                for (i in 2 until points.size step 2) lineTo(points[i] * u, points[i + 1] * u)
                if (close) close()
            }
            if (filled) drawPath(p, color) else drawPath(p, color, style = Stroke(1.7f * u, cap = StrokeCap.Round))
        }
        fun line(x1: Float, y1: Float, x2: Float, y2: Float) = path(x1, y1, x2, y2)
        fun circle(x: Float, y: Float, r: Float, fill: Boolean = false) {
            if (fill) drawCircle(color, r * u, Offset(x * u, y * u))
            else drawCircle(color, r * u, Offset(x * u, y * u), style = Stroke(1.55f * u))
        }
        when (glyph) {
            TvGlyph.Play -> path(7f, 3.5f, 20f, 12f, 7f, 20.5f, close = true, filled = true)
            TvGlyph.Pause -> for (x in listOf(6f, 13.5f)) drawRoundRect(color, Offset(x * u, 4f * u), Size(4.5f * u, 16f * u), CornerRadius(1.2f * u))
            TvGlyph.Previous -> { line(5f, 5f, 5f, 19f); path(19f, 4f, 7f, 12f, 19f, 20f, close = true, filled = true) }
            TvGlyph.Next -> { line(19f, 5f, 19f, 19f); path(5f, 4f, 17f, 12f, 5f, 20f, close = true, filled = true) }
            TvGlyph.Back -> path(15f, 5f, 8f, 12f, 15f, 19f)
            TvGlyph.Shuffle -> { path(3f, 6f, 7f, 6f, 16f, 18f, 21f, 18f); path(3f, 18f, 7f, 18f, 10f, 14f); path(13f, 10f, 16f, 6f, 21f, 6f); path(18f, 3f, 21f, 6f, 18f, 9f); path(18f, 15f, 21f, 18f, 18f, 21f) }
            TvGlyph.Repeat, TvGlyph.RepeatOne -> {
                path(4f, 10f, 4f, 6f, 20f, 6f); path(17f, 3f, 20f, 6f, 17f, 9f)
                path(20f, 14f, 20f, 18f, 4f, 18f); path(7f, 15f, 4f, 18f, 7f, 21f)
                if (glyph == TvGlyph.RepeatOne) path(10.5f, 11f, 12f, 10f, 12f, 14f)
            }
            TvGlyph.Volume -> { path(3f, 9f, 6f, 9f, 11f, 5f, 11f, 19f, 6f, 15f, 3f, 15f, close=true); path(15f, 8f, 17f, 12f, 15f, 16f); path(18f, 5f, 21f, 12f, 18f, 19f) }
            TvGlyph.Queue -> { line(3f, 5f, 20f, 5f); line(3f, 11f, 13f, 11f); line(3f, 17f, 10f, 17f); path(16f, 18f, 16f, 10f, 21f, 12f); circle(14f, 18f, 2f, true) }
            TvGlyph.Favorite -> drawPath(Path().apply {
                moveTo(12f*u,20f*u); cubicTo(8f*u,16f*u,2f*u,12f*u,2.5f*u,8f*u); cubicTo(3f*u,3f*u,9f*u,2f*u,12f*u,7f*u)
                cubicTo(15f*u,2f*u,21f*u,3f*u,21.5f*u,8f*u); cubicTo(22f*u,12f*u,16f*u,16f*u,12f*u,20f*u)
            }, color, style = Stroke(1.8f * u))
            TvGlyph.Home -> { path(3f, 11f, 12f, 3f, 21f, 11f); path(6f, 9f, 6f, 20f, 18f, 20f, 18f, 9f); line(12f, 20f, 12f, 14f) }
            TvGlyph.Discover -> { circle(12f, 12f, 9f); path(8f, 16f, 10f, 10f, 16f, 8f, 14f, 14f, close=true) }
            TvGlyph.Search -> { circle(10f, 10f, 6.5f); line(15f, 15f, 21f, 21f) }
            TvGlyph.Profile -> { circle(12f, 7f, 4f); path(4f, 21f, 5f, 17f, 8f, 14f, 16f, 14f, 19f, 17f, 20f, 21f) }
            TvGlyph.Add -> { line(4f, 6f, 20f, 6f); line(4f, 12f, 11f, 12f); line(4f, 18f, 10f, 18f); line(17f, 11f, 17f, 21f); line(12f, 16f, 22f, 16f) }
            TvGlyph.Lyrics -> { line(4f, 5f, 18f, 5f); line(4f, 10f, 21f, 10f); line(4f, 15f, 15f, 15f); line(4f, 20f, 18f, 20f) }
            TvGlyph.Lock -> { path(7f, 10f, 7f, 7f, 9f, 4f, 15f, 4f, 17f, 7f, 17f, 10f); path(5f, 10f, 19f, 10f, 19f, 21f, 5f, 21f, close = true); line(12f, 14f, 12f, 17f) }
            TvGlyph.Settings -> { line(3f, 6f, 21f, 6f); line(3f, 12f, 21f, 12f); line(3f, 18f, 21f, 18f); circle(9f, 6f, 2f, true); circle(15f, 12f, 2f, true); circle(9f, 18f, 2f, true) }
            TvGlyph.About -> { circle(12f, 12f, 9f); circle(12f, 8f, 1f, true); line(12f, 12f, 12f, 17f) }
            TvGlyph.Close -> { line(6f, 6f, 18f, 18f); line(6f, 18f, 18f, 6f) }
        }
    }
}

@Composable
internal fun StageTile(modifier: Modifier, enabled: Boolean = true, quiet: Boolean = false, onClick: () -> Unit, content: @Composable () -> Unit) {
    var focused by remember { mutableStateOf(false) }
    val shape = androidx.compose.foundation.shape.RoundedCornerShape(11.dp)
    val rim = Brush.verticalGradient(listOf(Color(0x557CC1DC), Color(0x1AB9D1E0), Color(0x12728B9F)))
    val edge = when {
        focused -> Modifier.border(1.6.dp, StageGlass, shape)
        quiet -> Modifier
        else -> Modifier.border(.8.dp, rim, shape)
    }
    Surface(onClick = onClick, enabled = enabled,
        modifier = modifier.onFocusChanged { focused = it.isFocused }.clip(shape).then(edge),
        shape = ClickableSurfaceDefaults.shape(shape),
        colors = ClickableSurfaceDefaults.colors(containerColor = if (quiet) Color.Transparent else Backdrop, focusedContainerColor = Color(0xFF2A3846)),
        scale = ClickableSurfaceDefaults.scale(focusedScale = 1f, pressedScale = 1f)) { content() }
}

private fun heartPath(x: Float, y: Float, width: Float, height: Float) = Path().apply {
    fun at(rx: Float, ry: Float) = Offset(x + width * rx, y + height * ry)
    fun curve(ax: Float, ay: Float, bx: Float, by: Float, cx: Float, cy: Float) {
        val a = at(ax,ay); val b = at(bx,by); val c = at(cx,cy)
        cubicTo(a.x,a.y,b.x,b.y,c.x,c.y)
    }
    moveTo(x+width*.50f, y+height*.95f)
    curve(.36f,.84f,.03f,.62f,.03f,.32f)
    curve(.03f,.08f,.29f,-.03f,.50f,.18f)
    curve(.71f,-.03f,.97f,.08f,.97f,.32f)
    curve(.97f,.62f,.64f,.84f,.50f,.95f)
    close()
}

@Composable
internal fun FavoriteBackdrop(modifier: Modifier) {
    Canvas(modifier) {
        drawRect(Brush.linearGradient(listOf(Color(0xFF765367), Color(0xFF45566B), Color(0xFF835C7B))))
        drawRect(Brush.radialGradient(listOf(Color(0xBBF39498), Color(0x447B5E73), Color.Transparent),
            center = Offset(size.width*.17f, size.height*.22f), radius = size.width*.33f))
        drawRect(Brush.radialGradient(listOf(Color(0xAAC08CB2), Color.Transparent),
            center = Offset(size.width*.92f, size.height*.22f), radius = size.width*.6f))
        val silhouette = heartPath(size.width*.72f, size.height*.17f, size.width*.47f, size.height*1.16f)
        drawPath(silhouette, Brush.linearGradient(listOf(Color(0x3CF7D3ED), Color(0x80FFACC4), Color(0xE2FFC2AD)),
            start = Offset(size.width*.75f, size.height*.25f), end = Offset(size.width, size.height)))
        val edge = Path().apply {
            moveTo(size.width*.70f,size.height)
            cubicTo(size.width*.79f,size.height*.79f,size.width*.90f,size.height*.67f,size.width,size.height*.61f)
            lineTo(size.width,size.height); close()
        }
        drawPath(edge, Brush.horizontalGradient(listOf(Color.Transparent,Color(0xB4FFD2C2))))
    }
}

@Composable
internal fun FavoriteHeart(modifier: Modifier) {
    Canvas(modifier) {
        drawCircle(Brush.radialGradient(listOf(Color(0xC8FF9FAD), Color.Transparent), radius = size.width*.5f))
        val heart = heartPath(size.width*.13f, size.height*.19f, size.width*.74f, size.height*.66f)
        drawPath(heart, Brush.verticalGradient(listOf(Color(0xFFF77E90), Color(0xFFFFACC0), Color(0xFFFFF0F2)),
            startY = size.height*.19f, endY = size.height*.86f))
        drawPath(heart, Color(0x86FFE2E2), style = Stroke(1.25.dp.toPx()))
    }
}
