package com.trackferry.tv

import android.content.Context
import android.view.View
import androidx.activity.compose.BackHandler
import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.tween
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.widthIn
import androidx.compose.runtime.collectAsState
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.graphics.BlendMode
import androidx.compose.ui.graphics.CompositingStrategy
import androidx.compose.ui.graphics.TransformOrigin
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.lerp
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.focusable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.LongState
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusProperties
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.onKeyEvent
import androidx.compose.ui.input.key.type
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.semantics.ProgressBarRangeInfo
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.progressBarRangeInfo
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.setProgress
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.media3.common.MediaItem
import androidx.media3.common.Player
import androidx.media3.session.MediaController
import androidx.tv.material3.Border
import androidx.tv.material3.Button
import androidx.tv.material3.ButtonDefaults
import androidx.tv.material3.MaterialTheme
import androidx.tv.material3.Text
import androidx.tv.material3.darkColorScheme
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

internal data class Playback(
    val itemIndex: Int,
    val items: List<MediaItem>,
    val mediaId: String?,
    val title: String,
    val artist: String,
    val album: String,
    val cover: String,
    val duration: Long,
    val state: Int,
    val playing: Boolean,
    val wantsPlay: Boolean,
    val seekable: Boolean,
    val failed: Boolean,
    val shuffle: Boolean,
    val repeatOne: Boolean,
)

internal fun Player.snapshot(items: List<MediaItem> = List(mediaItemCount, ::getMediaItemAt)) = Playback(
    currentMediaItemIndex, items, currentMediaItem?.mediaId,
    mediaMetadata.title?.toString().orEmpty(), mediaMetadata.artist?.toString().orEmpty(), mediaMetadata.albumTitle?.toString().orEmpty(),
    mediaMetadata.extras?.getString("cover").orEmpty(), duration.coerceAtLeast(0), playbackState, isPlaying,
    playWhenReady, isCurrentMediaItemSeekable, playerError != null, shuffleModeEnabled, repeatMode == Player.REPEAT_MODE_ONE,
)

@Composable
internal fun TrackFerryScreen(content: @Composable () -> Unit) {
    MaterialTheme(colorScheme = darkColorScheme(primary = Accent, onPrimary = Backdrop, surface = Backdrop, onSurface = Ink)) {
        Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
            TideBackdrop(Teal)
            content()
        }
    }
}

private enum class PlayerPanel { Main, Lyrics, Queue, Playlists, Sound }

@Composable
internal fun PlayerScreen(player: MediaController, back: () -> Unit, libraryChanged: () -> Unit) {
    val context = LocalContext.current
    val view = LocalView.current
    var playback by remember(player) { mutableStateOf(player.snapshot()) }
    val position = remember(player) { mutableLongStateOf(player.currentPosition.coerceAtLeast(0)) }
    var panel by remember { mutableStateOf(PlayerPanel.Main) }
    var from by remember { mutableStateOf(PlayerPanel.Main) }
    val playFocus = remember { FocusRequester() }
    val queueFocus = remember { FocusRequester() }
    val playlistFocus = remember { FocusRequester() }
    val lyricsFocus = remember { FocusRequester() }
    val seekFocus = remember { FocusRequester() }
    val soundFocus = remember { FocusRequester() }
    val dismiss = remember { FocusRequester() }
    DisposableEffect(player) {
        val listener = object : Player.Listener {
            override fun onEvents(player: Player, events: Player.Events) {
                playback = if (events.contains(Player.EVENT_TIMELINE_CHANGED)) player.snapshot() else player.snapshot(playback.items)
                position.longValue = player.currentPosition.coerceAtLeast(0)
            }
        }
        player.addListener(listener)
        playback = player.snapshot()
        position.longValue = player.currentPosition.coerceAtLeast(0)
        onDispose { player.removeListener(listener) }
    }
    KeepScreenWhilePlaying(view, playback.playing)
    LaunchedEffect(player, playback.playing) {
        while (playback.playing) {
            position.longValue = player.currentPosition.coerceAtLeast(0)
            delay(33)
        }
    }
    LaunchedEffect(panel) {
        withFrameNanos {}
        runCatching {
            if (panel != PlayerPanel.Main) dismiss.requestFocus() else when (from) {
                PlayerPanel.Queue -> queueFocus.requestFocus()
                PlayerPanel.Lyrics -> lyricsFocus.requestFocus()
                PlayerPanel.Sound -> soundFocus.requestFocus()
                PlayerPanel.Playlists -> if (playback.mediaId?.startsWith("nas/") == true) playlistFocus.requestFocus() else playFocus.requestFocus()
                else -> playFocus.requestFocus()
            }
        }
    }
    fun close() { from = panel; panel = PlayerPanel.Main }
    BackHandler(panel != PlayerPanel.Main, onBack = ::close)
    val local = Tracks.firstOrNull { it.id == playback.mediaId }
    val nas = NasPlayback.connection
    val nasTrack = playback.mediaId?.takeIf { it.startsWith("nas/") }?.split('/')?.getOrNull(1)?.toLongOrNull()
    var favorite by remember(nas, nasTrack) { mutableStateOf<Boolean?>(null) }
    var writing by remember(nas, nasTrack) { mutableStateOf(false) }
    var notice by remember { mutableStateOf("") }
    var playlistSong by remember { mutableStateOf<PlaylistSong?>(null) }
    val scope = rememberCoroutineScope()
    LaunchedEffect(nas, nasTrack) {
        if (nas == null || nasTrack == null) return@LaunchedEffect
        try { favorite = withContext(Dispatchers.IO) { nas.favorite(nasTrack) } }
        catch (e: CancellationException) { throw e }
        catch (_: Exception) { /* a press can retry once the library is back */ }
    }
    LaunchedEffect(notice) { if (notice.isNotEmpty()) { delay(3800); notice = "" } }
    fun saveFavorite() {
        if (nas == null || nasTrack == null || writing) return
        writing = true
        scope.launch {
            try {
                val current = favorite
                val saved = withContext(Dispatchers.IO) { nas.setFavorite(nasTrack, !(current ?: nas.favorite(nasTrack))) }
                favorite = saved
                notice = if (saved) "已收藏" else "已取消收藏"
                libraryChanged()
            } catch (e: CancellationException) { throw e }
            catch (e: Exception) { notice = e.tvMessage("暂时无法更新收藏") }
            finally { writing = false }
        }
    }
    var remoteWords by remember(playback.mediaId, nas) { mutableStateOf<TrackLyrics?>(null) }
    var loadingLyrics by remember(playback.mediaId) { mutableStateOf(nasTrack != null) }
    LaunchedEffect(playback.mediaId, nas) {
        if (nasTrack == null) return@LaunchedEffect
        loadingLyrics = true
        try { remoteWords = withContext(Dispatchers.IO) { nas?.lyrics(nasTrack) } }
        catch (e: CancellationException) { throw e }
        catch (_: Exception) { /* Lyrics never block playback. */ }
        finally { loadingLyrics = false }
    }
    val lyrics = if (local != null) remember(context, local.id) { loadLyrics(context, local) } else remoteWords?.lines.orEmpty()
    // The offset belongs to the song and is kept by the server, so every screen shows it alike.
    // The old setting for all songs on this television is gone.
    LaunchedEffect(Unit) { context.getSharedPreferences("lyrics", Context.MODE_PRIVATE).edit().remove("tv_offset").apply() }
    var offset by remember(playback.mediaId) { mutableLongStateOf(0L) }
    var saved by remember(playback.mediaId) { mutableLongStateOf(0L) }
    LaunchedEffect(remoteWords) { remoteWords?.let { offset = it.offset; saved = it.offset } }
    LaunchedEffect(nasTrack, offset) {
        if (nas == null || nasTrack == null || offset == saved) return@LaunchedEffect
        delay(700) // one write after the presses stop
        for (attempt in 0 until 3) {
            try {
                val kept = withContext(Dispatchers.IO) { nas.setLyricsOffset(nasTrack, offset) }
                saved = kept
                if (kept != offset) offset = kept
                return@LaunchedEffect
            } catch (e: CancellationException) { throw e }
            catch (e: Exception) {
                if (e is TvHttpError && e.status in 400..499) { notice = e.tvMessage("这首歌的歌词偏移没存上"); return@LaunchedEffect }
                delay(1_500L * (attempt + 1))
            }
        }
        notice = "歌词偏移暂时没存上，网络好了再按一下"
    }
    fun adjust(by: Long) { offset = (offset + by).coerceIn(-60_000L, 60_000L) }
    val bitmap = rememberArtwork(nas, playback.cover, large = true)
    val seed = playback.album.ifBlank { playback.title }
    val tone = remember(bitmap, seed) { coverTone(bitmap, seed) }
    val seek: (Long) -> Unit = { target -> if (playback.seekable) player.seekTo(target.coerceIn(0, playback.duration)) }
    val status = when {
        playback.failed -> "播放失败，可以重试"
        playback.state == Player.STATE_BUFFERING -> "正在缓冲"
        playback.playing -> "正在播放"
        playback.state == Player.STATE_ENDED -> "已播完"
        playback.state == Player.STATE_IDLE -> "等待播放"
        else -> "已暂停"
    }
    val preset by SoundSettings.preset.collectAsState()
    BoxWithConstraints(Modifier.fillMaxSize()) {
        val compact = maxHeight < 580.dp
        val short = maxHeight < 450.dp
        val gutter = maxWidth * .075f
        val coverSide = minOf(maxWidth * .33f, maxHeight * .50f)
        val index by remember(lyrics, position, offset) { derivedStateOf { lineIndex(lyrics, position.longValue + offset) } }
        AmbientStage(bitmap, tone, playback.playing, focus = Offset(.25f, .46f))
        Column(Modifier.fillMaxSize().padding(horizontal = gutter, vertical = if (short) 10.dp else 17.dp)) {
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween, verticalAlignment = Alignment.CenterVertically) {
                Row(horizontalArrangement = Arrangement.spacedBy(4.dp), verticalAlignment = Alignment.CenterVertically) {
                    GlyphButton(if (panel == PlayerPanel.Main) "返回曲库" else "返回此刻", TvGlyph.Back,
                        if (panel == PlayerPanel.Main) Modifier else Modifier.focusRequester(dismiss),
                        onClick = if (panel == PlayerPanel.Main) back else ::close)
                    FerryBrand()
                }
                Row(horizontalArrangement = Arrangement.spacedBy(7.dp), verticalAlignment = Alignment.CenterVertically) {
                    if (panel == PlayerPanel.Main) {
                        GlyphButton(if (favorite == true) "取消收藏" else "收藏", TvGlyph.Favorite,
                            selected = favorite == true, enabled = nas != null && nasTrack != null && !writing, onClick = ::saveFavorite)
                        GlyphButton("播放队列", TvGlyph.Queue, Modifier.focusRequester(queueFocus), enabled = playback.items.isNotEmpty()) { panel = PlayerPanel.Queue }
                        if (nas != null && nasTrack != null) StageNav("加入歌单", TvGlyph.Add, Modifier.focusRequester(playlistFocus)) {
                            playlistSong = PlaylistSong(nasTrack, playback.title)
                            panel = PlayerPanel.Playlists
                        }
                        StageNav("音效 · ${eqPreset(preset).label}", TvGlyph.Volume, Modifier.focusRequester(soundFocus)) { panel = PlayerPanel.Sound }
                        StageNav("全屏歌词", TvGlyph.Lyrics, Modifier.focusRequester(lyricsFocus)) { panel = PlayerPanel.Lyrics }
                    } else if (panel == PlayerPanel.Lyrics && lyrics.isNotEmpty()) {
                        Text(when {
                            offset == 0L -> "这首歌未调整"
                            offset > 0 -> "这首歌提前 ${offsetText(offset)}"
                            else -> "这首歌延后 ${offsetText(-offset)}"
                        } + if (nasTrack != null && offset != saved) " · 保存中" else "", color = Muted, fontSize = 13.sp)
                        TvButton("延后 0.1 秒") { adjust(-100) }
                        TvButton("提前 0.1 秒") { adjust(100) }
                        if (offset != 0L) TvButton("还原") { offset = 0 }
                    }
                }
            }
            when (panel) {
                PlayerPanel.Playlists -> Box(Modifier.weight(1f).fillMaxWidth().padding(top = 18.dp)) {
                    val song = playlistSong
                    if (nas != null && song != null) TvPlaylistPicker(nas, song) { message ->
                        notice = message
                        libraryChanged()
                        close()
                    }
                }
                PlayerPanel.Sound -> Box(Modifier.weight(1f).fillMaxWidth().padding(top = 18.dp), contentAlignment = Alignment.TopCenter) {
                    SoundPanel(short, Modifier.widthIn(max = 720.dp))
                }
                PlayerPanel.Lyrics -> FullLyrics(lyrics, position, offset, playback, bitmap, seed, loadingLyrics, short, Modifier.weight(1f).fillMaxWidth())
                else -> Row(
                    Modifier.weight(1f).fillMaxWidth().padding(top = if (short) 6.dp else 14.dp, bottom = if (short) 4.dp else 12.dp),
                    horizontalArrangement = Arrangement.spacedBy(if (short) 30.dp else 60.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    val breath = rememberBreath(playback.playing)
                    GlowCover(seed, bitmap, tone, Modifier.size(coverSide).graphicsLayer {
                        val b = breath.floatValue; scaleX = b; scaleY = b
                    }, radius = 20, glow = if (short) 22.dp else 40.dp)
                    Column(Modifier.weight(1f).fillMaxHeight(), verticalArrangement = Arrangement.Top) {
                        if (panel == PlayerPanel.Queue) {
                            Text("播放队列", color = Ink, fontFamily = FontFamily.Serif, fontSize = 30.sp, fontWeight = FontWeight.SemiBold)
                            Text("${playback.items.size} 首", Modifier.padding(top = 4.dp, bottom = 8.dp), color = Muted, fontSize = 14.sp)
                            QueuePanel(playback) { i -> player.seekToDefaultPosition(i); player.play(); close() }
                        } else {
                            Row(Modifier.padding(top = if (short) 2.dp else 10.dp), horizontalArrangement = Arrangement.spacedBy(10.dp), verticalAlignment = Alignment.CenterVertically) {
                                BassDot(Modifier.size(14.dp), playback.playing)
                                Text(status, color = Accent, fontSize = if (short) 12.sp else 14.sp, fontWeight = FontWeight.SemiBold, letterSpacing = 2.sp)
                            }
                            Text(playback.title.ifBlank { "等待播放" }, Modifier.padding(top = if (short) 6.dp else 12.dp), color = Ink, fontFamily = FontFamily.Serif,
                                fontSize = when { short -> 30.sp; compact -> 40.sp; else -> 52.sp }, lineHeight = when { short -> 36.sp; compact -> 48.sp; else -> 60.sp },
                                fontWeight = FontWeight.SemiBold, letterSpacing = (-.5).sp, maxLines = 2, overflow = TextOverflow.Ellipsis)
                            val credit = listOf(playback.artist, playback.album).filter { it.isNotBlank() }.joinToString("  ·  ")
                            if (credit.isNotBlank()) Text(credit, Modifier.padding(top = 6.dp), color = Ink.copy(alpha = .70f),
                                fontSize = if (short) 15.sp else 18.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
                            LyricFlow(lyrics, index, position, offset, loadingLyrics, tone, Modifier.weight(1f).fillMaxWidth().padding(top = if (short) 8.dp else 18.dp),
                                size = when { short -> 20.sp; compact -> 24.sp; else -> 30.sp }, gap = if (short) 8.dp else 14.dp, lead = .40f)
                        }
                    }
                }
            }
            if (panel == PlayerPanel.Main) {
                // Only while music is heard; at rest it would be a row of dots.
                val heard = animateFloatAsState(if (playback.playing) 1f else 0f, tween(500), label = "spectrum shown")
                SpectrumWave(Modifier.fillMaxWidth().height(if (short) 30.dp else if (compact) 46.dp else 60.dp).graphicsLayer { alpha = heard.value }, playback.playing,
                    tone = lerp(Accent, tone, .3f))
                FishSeekBar(position, playback.duration, playback.seekable,
                    Modifier.fillMaxWidth().focusRequester(seekFocus).focusProperties { down = playFocus }, { player.currentPosition }, seek)
                Row(Modifier.fillMaxWidth().padding(top = if (short) 0.dp else 4.dp, bottom = if (short) 1.dp else 9.dp),
                    horizontalArrangement = Arrangement.spacedBy(if (short) 12.dp else 28.dp, Alignment.CenterHorizontally), verticalAlignment = Alignment.CenterVertically) {
                    val shuffle = playback.shuffle
                    val repeat = playback.repeatOne
                    Text(clock(position.longValue), Modifier.width(52.dp), color = Ink.copy(alpha = .8f), fontSize = 13.sp)
                    GlyphButton("随机播放", TvGlyph.Shuffle, selected = shuffle) { player.setMode(if (shuffle) "loop" else "shuffle") }
                    GlyphButton("上一首", TvGlyph.Previous, enabled = player.hasPreviousMediaItem()) { player.seekToPreviousMediaItem() }
                    GlyphButton(if (playback.wantsPlay && playback.state != Player.STATE_ENDED) "暂停" else "播放", if (playback.wantsPlay && playback.state != Player.STATE_ENDED) TvGlyph.Pause else TvGlyph.Play,
                        Modifier.focusRequester(playFocus).focusProperties { if (playback.seekable) up = seekFocus }, size = if (short) 46.dp else 64.dp, glass = true) {
                        when {
                            playback.failed || playback.state == Player.STATE_IDLE -> { player.prepare(); player.play() }
                            playback.state == Player.STATE_ENDED -> { player.seekToDefaultPosition(0); player.play() }
                            playback.wantsPlay -> player.pause()
                            else -> player.play()
                        }
                    }
                    GlyphButton("下一首", TvGlyph.Next, enabled = player.hasNextMediaItem()) { player.seekToNextMediaItem() }
                    GlyphButton(if (repeat) "单曲循环" else "顺序循环", if (repeat) TvGlyph.RepeatOne else TvGlyph.Repeat, selected = repeat) { player.setMode(if (repeat) "loop" else "one") }
                    Text(clock(playback.duration), Modifier.width(52.dp), color = Ink.copy(alpha = .8f), fontSize = 13.sp, textAlign = TextAlign.End)
                }
            }
        }
        if (notice.isNotBlank() || playback.failed) Text(notice.ifBlank { status }, Modifier.align(Alignment.TopCenter).padding(top = 15.dp)
            .clip(RoundedCornerShape(19.dp)).background(Color(0xEC17303D)).padding(horizontal = 15.dp, vertical = 8.dp), color = Ink, fontSize = 14.sp)
    }
}

private fun offsetText(ms: Long) = if (ms % 1000 == 0L) "${ms / 1000} 秒" else "%.1f 秒".format(java.util.Locale.ROOT, ms / 1000.0)

@Composable
private fun QueuePanel(playback: Playback, choose: (Int) -> Unit) {
    val list = rememberLazyListState()
    LaunchedEffect(Unit) { if (playback.itemIndex in playback.items.indices) list.scrollToItem(playback.itemIndex) }
    LazyColumn(state = list, contentPadding = PaddingValues(horizontal = 5.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(7.dp)) {
        itemsIndexed(playback.items, key = { _, item -> item.mediaId }) { i, item ->
            val current = i == playback.itemIndex
            FerryTile(Modifier.fillMaxWidth(), { choose(i) }) {
                Row(Modifier.padding(horizontal = 13.dp, vertical = 11.dp), horizontalArrangement = Arrangement.spacedBy(10.dp), verticalAlignment = Alignment.CenterVertically) {
                    Text(if (current) "▶" else "${i + 1}", color = if (current) CurrentTrackInk else Muted, fontSize = 15.sp)
                    Column(Modifier.weight(1f)) {
                        TrackTitle(item.mediaMetadata.title?.toString().orEmpty(), current, current && playback.playing, 18.sp)
                        item.mediaMetadata.artist?.toString()?.takeIf { it.isNotBlank() }?.let { Text(it, color = Muted, fontSize = 13.sp, maxLines = 1) }
                    }
                }
            }
        }
    }
}

@Composable
private fun KeepScreenWhilePlaying(view: View, playing: Boolean) {
    DisposableEffect(view, playing) {
        val previously = view.keepScreenOn
        if (playing) view.keepScreenOn = true
        onDispose { view.keepScreenOn = previously }
    }
}

private fun loadLyrics(context: Context, track: DemoTrack): List<LyricLine> =
    context.resources.openRawResource(track.lyrics).bufferedReader().use { parseLyrics(it.readText()) }

/**
 * 全屏歌词: the whole song scrolls by itself so the line being sung sits a little above the middle,
 * large and lit; the song, its cover and the 小鱼 line stay small at the side.
 */
@Composable
private fun FullLyrics(lines: List<LyricLine>, position: LongState, offset: Long, playback: Playback, bitmap: android.graphics.Bitmap?,
                       seed: String, loading: Boolean, short: Boolean, modifier: Modifier) {
    val index by remember(lines, position, offset) { derivedStateOf { lineIndex(lines, position.longValue + offset) } }
    Row(modifier.padding(top = if (short) 8.dp else 14.dp), horizontalArrangement = Arrangement.spacedBy(if (short) 24.dp else 44.dp)) {
        Column(Modifier.width(if (short) 170.dp else 230.dp).fillMaxHeight(), verticalArrangement = Arrangement.spacedBy(10.dp, Alignment.CenterVertically)) {
            GlowCover(seed, bitmap, coverTone(bitmap, seed), Modifier.fillMaxWidth().aspectRatio(1f), radius = 16, glow = 26.dp)
            Text(playback.title, color = Ink, fontFamily = FontFamily.Serif, fontSize = if (short) 18.sp else 22.sp, fontWeight = FontWeight.SemiBold, maxLines = 2, overflow = TextOverflow.Ellipsis)
            if (playback.artist.isNotBlank()) Text(playback.artist, color = Ink.copy(alpha = .7f), fontSize = 14.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
            FishTrack(position, playback.duration, Modifier.fillMaxWidth(), fish = 20.dp)
            SpectrumWave(Modifier.fillMaxWidth().height(34.dp), playback.playing, tone = Accent, points = 16)
        }
        LyricFlow(lines, index, position, offset, loading, Accent, Modifier.weight(1f).fillMaxHeight(), size = if (short) 32.sp else 44.sp, gap = if (short) 10.dp else 16.dp, lead = .38f)
    }
}

/**
 * The song's lines flowing past by themselves: the line being sung is large, warm and lit, the
 * rest fade with their distance and towards both edges. Every change is a layer property
 * (alpha, scale), so nothing is laid out again while it moves.
 */
@Composable
private fun LyricFlow(lines: List<LyricLine>, index: Int, position: LongState, offset: Long, loading: Boolean, tone: Color, modifier: Modifier,
                      size: androidx.compose.ui.unit.TextUnit, gap: androidx.compose.ui.unit.Dp, lead: Float) {
    BoxWithConstraints(modifier) {
        if (lines.isEmpty()) {
            Text(if (loading) "歌词读取中…" else "这首歌没有同步歌词", Modifier.align(Alignment.CenterStart), color = Muted, fontSize = size * .72f)
            return@BoxWithConstraints
        }
        val list = rememberLazyListState()
        val top = maxHeight * lead
        val tail = (maxHeight - top - 40.dp).coerceAtLeast(0.dp)
        LaunchedEffect(index, lines) {
            val to = index.coerceAtLeast(0)
            if (kotlin.math.abs(list.firstVisibleItemIndex - to) > 8) list.scrollToItem(to) else list.animateScrollToItem(to)
        }
        val sung = remember(tone) { lerp(Color(0xFFFFD9B8), tone, .18f) }
        LazyColumn(Modifier.fillMaxSize().graphicsLayer { compositingStrategy = CompositingStrategy.Offscreen }
            .drawWithContent {
                drawContent()
                drawRect(Brush.verticalGradient(0f to Color.Transparent, .30f to Color.Black, .70f to Color.Black, 1f to Color.Transparent), blendMode = BlendMode.DstIn)
            },
            state = list, userScrollEnabled = false,
            contentPadding = PaddingValues(top = top, bottom = tail),
            verticalArrangement = Arrangement.spacedBy(gap)) {
            itemsIndexed(lines, key = { i, _ -> i }) { i, line ->
                val distance = kotlin.math.abs(i - index)
                val lit = i == index
                val alpha by animateFloatAsState(when (distance) { 0 -> 1f; 1 -> .46f; 2 -> .28f; else -> .17f }, tween(360), label = "lyric alpha")
                val scale by animateFloatAsState(if (lit) 1f else .80f, tween(420), label = "lyric scale")
                // How long this line is sung: until the next one, but an interlude does not count.
                val span = remember(line, lines) {
                    val next = lines.getOrNull(i + 1)?.atMs ?: (line.atMs + 4_000)
                    (next - line.atMs).coerceIn(400L, 600L + line.text.length * 420L)
                }
                Text(line.text.ifBlank { "♪" }, Modifier.graphicsLayer {
                    this.alpha = alpha; scaleX = scale; scaleY = scale
                    transformOrigin = TransformOrigin(0f, .5f)
                    if (lit) compositingStrategy = CompositingStrategy.Offscreen
                }.then(if (!lit) Modifier else Modifier.drawWithContent {
                    drawContent()
                    // The words light up as they are sung: warm behind the front, cool ahead of it,
                    // with a soft edge that moves with the music. Only this line, only a redraw.
                    val p = ((position.longValue + offset - line.atMs).toFloat() / span).coerceIn(0f, 1f)
                    val front = this.size.width * p
                    val soft = this.size.width * .08f + 24f
                    drawRect(Brush.horizontalGradient(
                        0f to sung, ((front - soft * 2.5f) / this.size.width).coerceIn(0f, 1f) to sung,
                        (front / this.size.width).coerceIn(0f, 1f) to Color(0xFFFFFBF6),
                        ((front + soft) / this.size.width).coerceIn(0f, 1f) to Color(0xFFEAF1F3).copy(alpha = .55f), 1f to Color(0xFFEAF1F3).copy(alpha = .55f)),
                        blendMode = BlendMode.SrcIn)
                }), color = Ink, fontSize = size, lineHeight = size * 1.24f,
                    fontWeight = if (lit) FontWeight.SemiBold else FontWeight.Medium)
            }
        }
    }
}


internal fun clock(ms: Long): String {
    val sec = ms / 1000
    return "${sec / 60}:${(sec % 60).toString().padStart(2, '0')}"
}

@Composable
internal fun TvButton(label: String, modifier: Modifier = Modifier, enabled: Boolean = true, primary: Boolean = false, selected: Boolean = false, onClick: () -> Unit) {
    Button(
        onClick = onClick, enabled = enabled, modifier = modifier,
        colors = ButtonDefaults.colors(
            containerColor = if (primary) Ink else if (selected) Color.White.copy(alpha = .17f) else Glass,
            contentColor = if (primary) Backdrop else Ink,
            focusedContainerColor = Accent, focusedContentColor = Backdrop,
        ),
        border = ButtonDefaults.border(focusedBorder = Border(androidx.compose.foundation.BorderStroke(1.dp, Ink))),
        scale = ButtonDefaults.scale(focusedScale = 1.055f, pressedScale = .98f),
    ) { Text(label, fontSize = 16.sp, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis) }
}
