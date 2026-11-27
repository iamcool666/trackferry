package com.trackferry.tv

import androidx.compose.animation.togetherWith
import androidx.compose.foundation.gestures.animateScrollBy
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.tween
import androidx.compose.ui.graphics.graphicsLayer
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.focusGroup
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.itemsIndexed
import androidx.compose.foundation.lazy.grid.rememberLazyGridState
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.focusRestorer
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.graphics.lerp
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.media3.common.MediaItem
import androidx.media3.common.Player
import androidx.tv.material3.ClickableSurfaceDefaults
import androidx.tv.material3.Surface
import androidx.tv.material3.Text
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlin.random.Random

internal enum class TvSection { Home, Search, Mine }

/** The three ways in on the home page. The tile plays at once; 更多 opens the page with 换一批. */
internal enum class Discover(val title: String, val note: String, val source: String) {
    Picks("随便听听", "曲库里随手挑的十二首，每位歌手一首", ""),
    Albums("随机专辑", "从曲库里随手翻出的专辑", "album"),
    Artists("随机歌手", "今天听听谁", "artist"),
}

internal class TvBrowseState {
    var shelf by mutableStateOf<Shelf?>(null)
    var section by mutableStateOf(TvSection.Home)
    var discover by mutableStateOf<Discover?>(null)
    var collection by mutableStateOf<Collection?>(null)
    var tracks by mutableStateOf<List<NasTrack>?>(null)
    val search = TvSearchState()
    var batch by mutableLongStateOf(Random.nextLong(1, Long.MAX_VALUE))
    var shelfBatch = 0L
    var focus = CatalogFocus()
    var discoverFocus = 0
    var trackFocus = 0
    /** Where the home page was scrolled to, kept while another page is open. */
    var homeIndex = 0
    var homeOffset = 0
    /** Which album and which singer the entries put on next; they move on with every press. */
    var albumTurn by mutableIntStateOf(0)
    var artistTurn by mutableIntStateOf(0)

    fun closeCollection() {
        collection = null
        tracks = null
    }

    fun entries(kind: Discover) = shelf?.collections.orEmpty().filter { it.source == kind.source }
    fun nextOf(kind: Discover): Collection? {
        val list = entries(kind)
        if (list.isEmpty()) return null
        return list[(if (kind == Discover.Albums) albumTurn else artistTurn) % list.size]
    }
}

internal data class CatalogFocus(val rail: String = "now", val index: Int = 0)

@Composable
internal fun TvLibraryScreen(
    nas: NasConnection,
    state: TvBrowseState,
    start: (List<NasTrack>, Int, Boolean) -> Boolean,
    player: Player,
    now: () -> Unit,
    reconnect: () -> Unit,
    revoked: () -> Unit,
) {
    var refresh by remember { mutableIntStateOf(0) }
    var error by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var starting by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val selected = state.collection
    val section = state.section
    val discover = state.discover
    val atHome = selected == null && discover == null && section == TvSection.Home
    val navFocus = remember { FocusRequester() }
    fun land(to: TvSection) {
        if (section == to && selected == null && discover == null) return
        state.closeCollection()
        state.discover = null
        state.section = to
        if (to == TvSection.Home) { state.focus = CatalogFocus(); state.homeIndex = 0; state.homeOffset = 0 }
        error = ""
    }
    fun play(tracks: List<NasTrack>, index: Int, fullscreen: Boolean = true) {
        if (!start(tracks, index, fullscreen)) error = if (tracks.size > MAX_QUEUE) "队列超过 ${MAX_QUEUE} 首，请选一个较短的列表" else "无法开始播放，请重试"
    }
    fun open(item: Collection) {
        state.trackFocus = 0
        state.tracks = null
        state.collection = item
    }
    fun playList(item: Collection, tag: String) {
        if (starting != null) return
        starting = tag
        error = ""
        scope.launch {
            try {
                val tracks = withContext(Dispatchers.IO) { nas.queue(item) }
                if (tracks.isEmpty()) error = "「${item.title}」里没有能放的歌" else play(tracks, 0)
            } catch (e: CancellationException) { throw e }
            catch (e: Exception) { if (e is TvHttpError && e.status == 401) revoked() else error = e.tvMessage("暂时放不了「${item.title}」") }
            finally { starting = null }
        }
    }
    fun playEntry(kind: Discover) {
        val shelf = state.shelf ?: return
        if (kind == Discover.Picks) { if (shelf.picks.isNotEmpty()) play(shelf.picks, 0); return }
        val next = state.nextOf(kind) ?: return
        if (kind == Discover.Albums) state.albumTurn++ else state.artistTurn++
        playList(next, kind.name)
    }
    val batch = state.batch
    LaunchedEffect(nas, selected, section, refresh, batch) {
        error = ""
        busy = false
        if (if (selected != null) state.tracks != null else state.shelf != null && state.shelfBatch == batch) return@LaunchedEffect
        busy = true
        try {
            if (selected != null) state.tracks = withContext(Dispatchers.IO) { nas.queue(selected) }
            else {
                state.shelf = withContext(Dispatchers.IO) { nas.shelf(batch) }
                state.shelfBatch = batch
                state.albumTurn = 0
                state.artistTurn = 0
            }
        } catch (e: CancellationException) { throw e }
        catch (e: Exception) {
            if (e is TvHttpError && e.status == 401) revoked()
            else if (selected != null || section != TvSection.Search) error = e.tvMessage("读不到曲库，请检查服务器连接")
        } finally { busy = false }
    }
    BackHandler(!atHome) {
        when {
            selected != null -> state.closeCollection()
            discover != null -> state.discover = null
            else -> land(TvSection.Home)
        }
    }
    BoxWithConstraints(Modifier.fillMaxSize()) {
        val gutter = maxWidth * .048f
        val short = maxHeight < 450.dp
        val art = rememberArtwork(nas, selected?.cover.orEmpty(), large = true)
        // The stage behind every page: the collection being looked at, else what plays, else a pick.
        val heard = rememberPlayback(player, withItems = false)
        val playingArt = rememberArtwork(nas, heard.cover, large = true)
        val pickArt = rememberArtwork(nas, if (heard.mediaId == null) state.shelf?.picks?.firstOrNull { it.cover.isNotBlank() }?.cover.orEmpty() else "", large = true)
        val stage = art ?: playingArt ?: pickArt
        val tone = remember(stage, selected, heard.album) { coverTone(stage, selected?.title ?: heard.album.ifBlank { "曲库" }) }
        AmbientStage(stage, tone, heard.playing, focus = androidx.compose.ui.geometry.Offset(.24f, .42f))
        Column(Modifier.fillMaxSize()) {
            Row(Modifier.fillMaxWidth().padding(horizontal = gutter, vertical = if (short) 9.dp else 14.dp), horizontalArrangement = Arrangement.SpaceBetween, verticalAlignment = Alignment.CenterVertically) {
                FerryBrand()
                Row(horizontalArrangement = Arrangement.spacedBy(if (short) 8.dp else 14.dp), verticalAlignment = Alignment.CenterVertically) {
                    StageNav("首页", TvGlyph.Home, Modifier.focusRequester(navFocus), selected = section == TvSection.Home) { land(TvSection.Home) }
                    StageNav("搜索", TvGlyph.Search, selected = section == TvSection.Search) { land(TvSection.Search) }
                    StageNav("设置", TvGlyph.Settings, selected = section == TvSection.Mine) { land(TvSection.Mine) }
                }
                StageClock()
            }
            if (error.isNotBlank()) {
                Row(Modifier.padding(horizontal = gutter, vertical = 4.dp), horizontalArrangement = Arrangement.spacedBy(10.dp), verticalAlignment = Alignment.CenterVertically) {
                    Text(error, color = Accent, fontSize = 15.sp)
                    TvButton("重试") { error = ""; refresh++ }
                }
            }
            BoxWithConstraints(Modifier.weight(1f).fillMaxWidth()) {
                when {
                    selected != null -> TrackBrowser(nas, player, selected, state, busy, art, gutter) { tracks, index -> play(tracks, index) }
                    discover != null -> DiscoverPage(nas, state, discover, busy, starting, gutter, short, player,
                        play = { tracks, index -> play(tracks, index) }, playEntry = { playEntry(discover) }, open = ::open,
                        shuffle = { if (!busy) state.batch = Random.nextLong(1, Long.MAX_VALUE) }, back = { state.discover = null })
                    section == TvSection.Search -> TvSearchScreen(nas, state.search, gutter, short, revoked, ::open,
                        playOne = { play(listOf(it), 0) })
                    section == TvSection.Mine -> TvMineScreen(nas, state.shelf, busy, gutter, short, ::open, reconnect)
                    else -> Catalog(nas, state, gutter, short, maxHeight, busy, starting, player, now, navFocus,
                        playEntry = ::playEntry, more = { state.discoverFocus = 0; state.discover = it }, open = ::open,
                        startPicks = { playEntry(Discover.Picks) }, playPick = { i -> state.shelf?.picks?.let { play(it, i) } },
                        search = { land(TvSection.Search) })
                }
            }
            if (!atHome) NowCapsule(player, nas, gutter, now)
        }
    }
}

@Composable
internal fun StageNav(label: String, glyph: TvGlyph, modifier: Modifier = Modifier, selected: Boolean = false, onClick: () -> Unit) {
    val shape = RoundedCornerShape(24.dp)
    var focused by remember { mutableStateOf(false) }
    Surface(onClick = onClick, modifier = modifier.onFocusChanged { focused = it.isFocused }
        .then(if (selected || focused) Modifier.border(1.dp, StageGlass, shape) else Modifier),
        shape = ClickableSurfaceDefaults.shape(shape),
        colors = ClickableSurfaceDefaults.colors(containerColor = if (selected) Glass else Color.Transparent, focusedContainerColor = Color.White.copy(alpha = .14f)),
        scale = ClickableSurfaceDefaults.scale(focusedScale = 1f, pressedScale = 1f)) {
        Row(Modifier.padding(horizontal = 11.dp, vertical = 8.dp), horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
            StageIcon(glyph, Modifier.size(15.dp), if (selected || focused) Ink else Muted)
            Text(label, color = if (selected || focused) Ink else Muted, fontSize = 13.sp, maxLines = 1)
        }
    }
}

// ——— Home ———

/**
 * The home page reads like a stage: what plays (or an invitation to start) fills the top, large; the
 * ways in, the playlists and today's picks follow below and come up as the focus moves down.
 */
@Composable
private fun Catalog(
    nas: NasConnection, state: TvBrowseState, gutter: Dp, short: Boolean, height: Dp, busy: Boolean, starting: String?,
    player: Player, now: () -> Unit, navFocus: FocusRequester,
    playEntry: (Discover) -> Unit, more: (Discover) -> Unit, open: (Collection) -> Unit, startPicks: () -> Unit,
    playPick: (Int) -> Unit, search: () -> Unit,
) {
    val shelf = state.shelf
    val picks = shelf?.picks.orEmpty()
    val collections = shelf?.collections.orEmpty()
    val lists = collections.filter { it.source == "favorites" } + collections.filter { it.source == "playlist" }
    val nowFocus = remember { FocusRequester() }
    val entryFocus = remember { Discover.entries.associateWith { FocusRequester() } }
    val listFocus = remember { FocusRequester() }
    val pickFocus = remember { FocusRequester() }
    val target = remember { state.focus }
    val column = rememberLazyListState(state.homeIndex, state.homeOffset)
    DisposableEffect(Unit) { onDispose { state.homeIndex = column.firstVisibleItemIndex; state.homeOffset = column.firstVisibleItemScrollOffset } }
    LaunchedEffect(shelf != null) {
        withFrameNanos {}
        val to = when {
            shelf == null -> navFocus
            target.rail == "list" && lists.isNotEmpty() -> listFocus
            target.rail == "pick" && picks.isNotEmpty() -> pickFocus
            target.rail.startsWith("entry:") -> entryFocus[runCatching { Discover.valueOf(target.rail.removePrefix("entry:")) }.getOrNull()]
            else -> nowFocus
        } ?: nowFocus
        runCatching { to.requestFocus() }.onFailure { runCatching { column.scrollToItem(0); withFrameNanos {}; nowFocus.requestFocus() } }
    }
    // The stage and the whole 发现 row fit on the first screen; the playlists show their top below.
    val hero = (height * if (short) .60f else .58f).coerceAtLeast(200.dp)
    val heading = if (short) 15.sp else 19.sp
    // One progress for the whole arrival, so a row scrolled away and back never plays it again.
    val arrive = remember { androidx.compose.animation.core.Animatable(0f) }
    LaunchedEffect(Unit) { arrive.animateTo(1f, tween(1300, easing = androidx.compose.animation.core.LinearEasing)) }
    // Where the page rests for each row, decided here rather than left to "just bring the focused
    // card into view", which stops half way and can leave the stage cut off with no way back up.
    // The stage and 发现 share the first screen, so both rest at the very top; a lower row rests
    // with its heading just under the top. It runs a frame later so it wins over the default.
    val settle = rememberCoroutineScope()
    val density = androidx.compose.ui.platform.LocalDensity.current
    val headroom = with(density) { 10.dp.toPx() }
    fun rest(index: Int) {
        settle.launch {
            withFrameNanos {}
            if (index == 0) { column.animateScrollToItem(0); return@launch }
            val shown = column.layoutInfo.visibleItemsInfo.firstOrNull { it.index == index }
            if (shown != null) column.animateScrollBy(shown.offset - headroom, tween(420, easing = Settle))
            else column.animateScrollToItem(index)
        }
    }
    LazyColumn(Modifier.fillMaxSize(), state = column, contentPadding = PaddingValues(bottom = 40.dp),
        verticalArrangement = Arrangement.spacedBy(if (short) 14.dp else 22.dp)) {
        item("hero") {
            HomeHero(player, nas, picks, short, Modifier.fillMaxWidth().height(hero).padding(horizontal = gutter)
                .onFocusChanged { if (it.hasFocus) rest(0) }, nowFocus,
                onFocus = { state.focus = CatalogFocus("now") }, open = now, startPicks = startPicks, search = search, arrive = arrive)
        }
        item("discover") {
            Column(Modifier.padding(horizontal = gutter).rise(arrive, 3).onFocusChanged { if (it.hasFocus) rest(0) }, verticalArrangement = Arrangement.spacedBy(10.dp)) {
                ShelfHeading("发现", "按 OK 就放，往右到“更多”看全部", heading)
                Row(Modifier.fillMaxWidth().height(if (short) 88.dp else 112.dp), horizontalArrangement = Arrangement.spacedBy(16.dp)) {
                    Discover.entries.forEach { kind ->
                        val next = state.nextOf(kind)
                        val covers = when (kind) {
                            Discover.Picks -> picks.map { it.cover }
                            else -> state.entries(kind).let { all -> if (all.isEmpty()) emptyList() else List(minOf(3, all.size)) { all[(all.indexOf(next) + it) % all.size].cover } }
                        }
                        val line = when {
                            starting == kind.name -> "正在取歌…"
                            shelf == null -> if (busy) "正在读取曲库…" else "曲库暂时读不到"
                            kind == Discover.Picks -> if (picks.isEmpty()) "曲库还是空的" else "${picks.size} 首 · ${picks.take(2).joinToString("、") { it.title }}"
                            next == null -> "曲库里还没有"
                            kind == Discover.Albums -> "下一张 · ${next.title}"
                            else -> "下一位 · ${next.title}"
                        }
                        EntryTile(nas, kind, line, covers, enabled = shelf != null && (kind == Discover.Picks && picks.isNotEmpty() || next != null),
                            Modifier.weight(1f).fillMaxHeight(), entryFocus.getValue(kind), short,
                            onFocus = { state.focus = CatalogFocus("entry:${kind.name}") }, play = { playEntry(kind) }, more = { more(kind) })
                    }
                }
            }
        }
        item("lists") {
            PlaylistShelf(nas, lists, shelf == null && busy, short, heading, Modifier.fillMaxWidth().rise(arrive, 4).onFocusChanged { if (it.hasFocus) rest(2) }.padding(horizontal = gutter).height(if (short) 128.dp else 168.dp), listFocus,
                restore = target.index.takeIf { target.rail == "list" }, keep = { state.focus = CatalogFocus("list", it) }, open = open)
        }
        if (picks.isNotEmpty()) item("picks") {
            Column(Modifier.rise(arrive, 5).onFocusChanged { if (it.hasFocus) rest(3) }, verticalArrangement = Arrangement.spacedBy(10.dp)) {
                ShelfHeading("今天随手挑的", "${picks.size} 首 · 从哪首开始都行", heading, Modifier.padding(horizontal = gutter))
                val at = target.index.takeIf { target.rail == "pick" }?.coerceIn(picks.indices) ?: 0
                val row = rememberLazyListState(initialFirstVisibleItemIndex = at)
                val settle = rememberCoroutineScope()
                LazyRow(Modifier.fillMaxWidth().focusRestorer().focusGroup(), state = row, contentPadding = PaddingValues(horizontal = gutter, vertical = 6.dp),
                    horizontalArrangement = Arrangement.spacedBy(18.dp)) {
                    itemsIndexed(picks, key = { i, t -> "${t.id}/$i" }) { i, track ->
                        PickCard(nas, track, if (short) 104.dp else 138.dp, Modifier.then(if (i == at) Modifier.focusRequester(pickFocus) else Modifier)
                            .onFocusChanged { if (it.isFocused) { state.focus = CatalogFocus("pick", i); if (i == 0) settle.launch { row.animateScrollToItem(0) } } }) { playPick(i) }
                    }
                }
            }
        }
    }
}

@Composable
private fun ShelfHeading(title: String, note: String, size: androidx.compose.ui.unit.TextUnit, modifier: Modifier = Modifier) {
    Row(modifier, horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.Bottom) {
        Text(title, color = Ink, fontSize = size, fontWeight = FontWeight.SemiBold, letterSpacing = .5.sp)
        Text(note, Modifier.padding(bottom = 2.dp), color = Muted.copy(alpha = .8f), fontSize = 12.sp, maxLines = 1)
    }
}

/** What plays, as large as the page allows; with nothing playing, an invitation to start. */
@Composable
private fun HomeHero(player: Player, nas: NasConnection, picks: List<NasTrack>, short: Boolean, modifier: Modifier, focus: FocusRequester,
                     onFocus: () -> Unit, open: () -> Unit, startPicks: () -> Unit, search: () -> Unit,
                     arrive: androidx.compose.animation.core.Animatable<Float, *>) {
    val playback = rememberPlayback(player, withItems = false)
    val position = rememberPosition(player, playback, 250)
    val active = playback.mediaId != null
    val bitmap = rememberArtwork(nas, playback.cover, large = true)
    val seed = playback.album.ifBlank { playback.title.ifBlank { "TrackFerry" } }
    val tone = remember(bitmap, seed) { coverTone(bitmap, seed) }
    val playing = playback.wantsPlay && playback.state != Player.STATE_ENDED
    BoxWithConstraints(modifier) {
        val side = (maxHeight * .94f).coerceAtMost(maxWidth * .34f)
        Row(Modifier.fillMaxSize(), horizontalArrangement = Arrangement.spacedBy(if (short) 28.dp else 48.dp), verticalAlignment = Alignment.CenterVertically) {
            var focused by remember { mutableStateOf(false) }
            val breath = rememberBreath(playback.playing)
            Surface(onClick = if (active) open else startPicks, enabled = active || picks.isNotEmpty(),
                modifier = Modifier.size(side).rise(arrive, 0).graphicsLayer { val b = breath.floatValue; scaleX = b; scaleY = b }
                    .focusRequester(focus).onFocusChanged { focused = it.isFocused; if (it.isFocused) onFocus() },
                shape = ClickableSurfaceDefaults.shape(RoundedCornerShape(20.dp)),
                colors = ClickableSurfaceDefaults.colors(containerColor = Color.Transparent, focusedContainerColor = Color.Transparent, disabledContainerColor = Color.Transparent),
                scale = ClickableSurfaceDefaults.scale(focusedScale = 1.04f, pressedScale = .99f),
                glow = ClickableSurfaceDefaults.glow(),
                border = ClickableSurfaceDefaults.border(focusedBorder = androidx.tv.material3.Border(androidx.compose.foundation.BorderStroke(2.5.dp, StageGlass), shape = RoundedCornerShape(20.dp)))) {
                Box(Modifier.fillMaxSize().sheen(focused)) {
                    if (active) GlowCover(seed, bitmap, tone, Modifier.fillMaxSize(), radius = 20)
                    else PickMosaic(nas, picks, Modifier.fillMaxSize())
                    if (focused) Box(Modifier.align(Alignment.BottomStart).padding(14.dp).clip(RoundedCornerShape(20.dp)).background(Color(0xB00B1218))
                        .padding(horizontal = 12.dp, vertical = 6.dp)) {
                        Row(horizontalArrangement = Arrangement.spacedBy(7.dp), verticalAlignment = Alignment.CenterVertically) {
                            StageIcon(if (active) TvGlyph.Lyrics else TvGlyph.Play, Modifier.size(13.dp))
                            Text(if (active) "打开播放页" else "随便听听", color = Ink, fontSize = 13.sp, fontWeight = FontWeight.Medium)
                        }
                    }
                }
            }
            Column(Modifier.weight(1f).rise(arrive, 1), verticalArrangement = Arrangement.Center) {
                Row(horizontalArrangement = Arrangement.spacedBy(10.dp), verticalAlignment = Alignment.CenterVertically) {
                    if (active) BassDot(Modifier.size(14.dp), playback.playing)
                    Text(when {
                        !active -> "TRACKFERRY · 你的曲库"
                        playback.failed -> "播放出错了"
                        playback.state == Player.STATE_BUFFERING -> "正在缓冲"
                        playback.playing -> "正在播放"
                        playback.state == Player.STATE_ENDED -> "放完了"
                        else -> "已暂停"
                    }, color = Accent, fontSize = if (short) 12.sp else 14.sp, fontWeight = FontWeight.SemiBold, letterSpacing = 2.sp)
                }
                androidx.compose.animation.AnimatedContent(if (active) playback.title else "今晚，听点什么？", label = "hero title",
                    transitionSpec = {
                        (androidx.compose.animation.fadeIn(tween(520, 120)) + androidx.compose.animation.slideInVertically(tween(620, easing = Settle)) { it / 5 })
                            .togetherWith(androidx.compose.animation.fadeOut(tween(260)))
                    }) { title ->
                    Text(title, Modifier.padding(top = if (short) 6.dp else 10.dp), color = Ink, fontFamily = FontFamily.SansSerif,
                        fontSize = if (short) 30.sp else 42.sp, lineHeight = if (short) 35.sp else 48.sp, fontWeight = FontWeight.SemiBold,
                        maxLines = 2, overflow = TextOverflow.Ellipsis, letterSpacing = (-.5).sp)
                }
                Text(if (active) listOf(playback.artist, playback.album).filter { it.isNotBlank() }.joinToString("  ·  ")
                    else "按一下封面，从曲库里随手挑十二首；也可以用手机把歌送到这台电视。",
                    Modifier.padding(top = if (short) 4.dp else 6.dp).widthIn(max = 620.dp), color = Ink.copy(alpha = .72f),
                    fontSize = if (short) 14.sp else 17.sp, lineHeight = if (short) 20.sp else 24.sp, maxLines = 2, overflow = TextOverflow.Ellipsis)
                if (active) {
                    Column(Modifier.padding(top = if (short) 10.dp else 16.dp).widthIn(max = 640.dp)) {
                        FishTrack(position, playback.duration, Modifier.fillMaxWidth(), fish = if (short) 22.dp else 28.dp)
                        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                            Text(clock(position.longValue), color = Ink.copy(alpha = .62f), fontSize = 12.sp)
                            Text(clock(playback.duration), color = Ink.copy(alpha = .62f), fontSize = 12.sp)
                        }
                    }
                }
                Row(Modifier.padding(top = if (short) 8.dp else 14.dp).rise(arrive, 2), horizontalArrangement = Arrangement.spacedBy(14.dp), verticalAlignment = Alignment.CenterVertically) {
                    if (active) {
                        PillButton(if (playing) "暂停" else "播放", if (playing) TvGlyph.Pause else TvGlyph.Play, primary = true, short = short) {
                            when {
                                playback.failed || playback.state == Player.STATE_IDLE -> { player.prepare(); player.play() }
                                playback.state == Player.STATE_ENDED -> { player.seekToDefaultPosition(0); player.play() }
                                playing -> player.pause()
                                else -> player.play()
                            }
                        }
                        GlyphButton("上一首", TvGlyph.Previous, size = if (short) 40.dp else 48.dp, enabled = player.hasPreviousMediaItem()) { player.seekToPreviousMediaItem() }
                        GlyphButton("下一首", TvGlyph.Next, size = if (short) 40.dp else 48.dp, enabled = player.hasNextMediaItem()) { player.seekToNextMediaItem() }
                        PillButton("歌词", TvGlyph.Lyrics, short = short, onClick = open)
                    } else {
                        PillButton("随便听听", TvGlyph.Play, primary = true, enabled = picks.isNotEmpty(), short = short, onClick = startPicks)
                        PillButton("搜索", TvGlyph.Search, short = short, onClick = search)
                    }
                }
            }
        }
    }
}

/** Four of today's picks as one cover, while nothing plays. */
@Composable
private fun PickMosaic(nas: NasConnection, picks: List<NasTrack>, modifier: Modifier) {
    val shape = RoundedCornerShape(20.dp)
    Column(modifier.clip(shape).border(1.dp, Color.White.copy(alpha = .14f), shape)) {
        for (r in 0..1) Row(Modifier.weight(1f)) {
            for (c in 0..1) {
                val track = picks.getOrNull(r * 2 + c)
                val bitmap = rememberArtwork(nas, track?.cover.orEmpty())
                FerryCover(track?.title ?: "TrackFerry ${r * 2 + c}", bitmap, Modifier.weight(1f).fillMaxHeight(), radius = 0)
            }
        }
    }
}

/** One of today's picks: its cover, and the song below it. */
@Composable
private fun PickCard(nas: NasConnection, track: NasTrack, side: Dp, modifier: Modifier, play: () -> Unit) {
    val bitmap = rememberArtwork(nas, track.cover)
    var focused by remember { mutableStateOf(false) }
    Column(Modifier.width(side), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Surface(onClick = play, modifier = modifier.size(side).onFocusChanged { focused = it.isFocused },
            shape = ClickableSurfaceDefaults.shape(RoundedCornerShape(14.dp)),
            colors = ClickableSurfaceDefaults.colors(containerColor = Color.Transparent, focusedContainerColor = Color.Transparent),
            scale = ClickableSurfaceDefaults.scale(focusedScale = 1.08f, pressedScale = .98f),
            border = ClickableSurfaceDefaults.border(focusedBorder = androidx.tv.material3.Border(androidx.compose.foundation.BorderStroke(2.dp, StageGlass), shape = RoundedCornerShape(14.dp)))) {
            Box(Modifier.fillMaxSize().sheen(focused)) {
                FerryCover(track.title, bitmap, Modifier.fillMaxSize(), radius = 14)
                if (focused) Box(Modifier.align(Alignment.Center).size(side * .32f).clip(CircleShape).background(Color(0xA80B1218)), contentAlignment = Alignment.Center) {
                    StageIcon(TvGlyph.Play, Modifier.size(side * .13f))
                }
            }
        }
        Column {
            Text(track.title, color = if (focused) Ink else Ink.copy(alpha = .88f), fontSize = 14.sp, fontWeight = FontWeight.Medium, maxLines = 1, overflow = TextOverflow.Ellipsis)
            if (track.artist.isNotBlank()) Text(track.artist, color = Muted, fontSize = 12.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    }
}

@Composable private fun Spacer(height: Dp) = Box(Modifier.height(height))

private val entryTones = mapOf(Discover.Picks to Color(0xFF5FA8B4), Discover.Albums to Color(0xFFD08C6C), Discover.Artists to Color(0xFF9A86C8))

/**
 * One way in, as one card: the next cover fills its right side and fades into the colour of the
 * entry; OK plays at once. 更多 is its own narrow button right beside it, one press of → away:
 * a long press on a remote repeats OK, so the page it opened took the release as a click.
 */
@OptIn(androidx.compose.ui.ExperimentalComposeUiApi::class)
@Composable
private fun EntryTile(nas: NasConnection, kind: Discover, line: String, covers: List<String>, enabled: Boolean, modifier: Modifier,
                      focus: FocusRequester, short: Boolean, onFocus: () -> Unit, play: () -> Unit, more: () -> Unit) {
    val tone = entryTones.getValue(kind)
    val base = lerp(Color(0xFF0C1217), tone, .30f)
    val shape = RoundedCornerShape(16.dp)
    var focused by remember { mutableStateOf(false) }
    val near = animateFloatAsState(if (focused) 1.08f else 1f, tween(700), label = "entry cover")
    Row(modifier, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
    Surface(onClick = play, enabled = enabled,
        modifier = Modifier.weight(1f).fillMaxHeight().focusRequester(focus).onFocusChanged { focused = it.isFocused; if (it.isFocused) onFocus() },
        shape = ClickableSurfaceDefaults.shape(shape),
        colors = ClickableSurfaceDefaults.colors(containerColor = base, focusedContainerColor = base, disabledContainerColor = base.copy(alpha = .6f)),
        scale = ClickableSurfaceDefaults.scale(focusedScale = 1.045f, pressedScale = .985f),
        border = ClickableSurfaceDefaults.border(
            border = androidx.tv.material3.Border(androidx.compose.foundation.BorderStroke(1.dp, Color.White.copy(alpha = .10f)), shape = shape),
            focusedBorder = androidx.tv.material3.Border(androidx.compose.foundation.BorderStroke(2.dp, StageGlass), shape = shape))) {
        BoxWithConstraints(Modifier.fillMaxSize().sheen(focused)) {
            val lead = covers.firstOrNull { it.isNotBlank() }
            val wide = maxWidth
            if (lead != null) {
                val bitmap = rememberArtwork(nas, lead, large = true)
                Box(Modifier.align(Alignment.CenterEnd).fillMaxHeight().width(maxWidth * .62f).graphicsLayer {
                    scaleX = near.value; scaleY = near.value; transformOrigin = androidx.compose.ui.graphics.TransformOrigin(.7f, .5f)
                }) { FerryCover(lead, bitmap, Modifier.fillMaxSize(), radius = 0) }
            }
            // The cover melts into the entry's colour; the words sit on the solid part.
            Box(Modifier.matchParentSize().background(Brush.horizontalGradient(0f to base, .38f to base, .72f to base.copy(alpha = .35f), 1f to base.copy(alpha = .05f))))
            Box(Modifier.matchParentSize().background(Brush.verticalGradient(0f to Color.Transparent, 1f to Color.Black.copy(alpha = .30f))))
            Column(Modifier.align(Alignment.CenterStart).fillMaxHeight().padding(horizontal = if (short) 14.dp else 20.dp, vertical = if (short) 10.dp else 14.dp),
                verticalArrangement = Arrangement.Center) {
                Text(kind.title, color = Ink, fontFamily = FontFamily.SansSerif, fontSize = if (short) 20.sp else 26.sp, fontWeight = FontWeight.SemiBold, maxLines = 1)
                Text(if (focused && enabled) "按 OK 播放" else line, Modifier.padding(top = 4.dp).widthIn(max = wide * .55f),
                    color = if (focused && enabled) Accent else Ink.copy(alpha = .70f), fontSize = if (short) 11.sp else 13.sp,
                    fontWeight = if (focused) FontWeight.Medium else FontWeight.Normal, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
    }
    var moreFocused by remember { mutableStateOf(false) }
    Surface(onClick = more, enabled = enabled,
        modifier = Modifier.width(if (short) 52.dp else 64.dp).fillMaxHeight().onFocusChanged { moreFocused = it.isFocused; if (it.isFocused) onFocus() },
        shape = ClickableSurfaceDefaults.shape(shape),
        colors = ClickableSurfaceDefaults.colors(containerColor = base.copy(alpha = .55f), focusedContainerColor = base, disabledContainerColor = base.copy(alpha = .3f)),
        scale = ClickableSurfaceDefaults.scale(focusedScale = 1.06f, pressedScale = .97f),
        border = ClickableSurfaceDefaults.border(
            border = androidx.tv.material3.Border(androidx.compose.foundation.BorderStroke(1.dp, Color.White.copy(alpha = .10f)), shape = shape),
            focusedBorder = androidx.tv.material3.Border(androidx.compose.foundation.BorderStroke(2.dp, StageGlass), shape = shape))) {
        Column(Modifier.fillMaxSize(), verticalArrangement = Arrangement.Center, horizontalAlignment = Alignment.CenterHorizontally) {
            Text("›", color = if (moreFocused) Accent else Ink, fontSize = if (short) 22.sp else 28.sp, fontWeight = FontWeight.Light)
            Text("更多", color = if (moreFocused) Accent else Ink.copy(alpha = .8f), fontSize = if (short) 11.sp else 13.sp, fontWeight = FontWeight.Medium)
        }
    }
    }
}

private val playlistFallbacks = listOf(R.drawable.tv_playlist_1, R.drawable.tv_playlist_2, R.drawable.tv_playlist_3, R.drawable.tv_playlist_4)

/**
 * 我的歌单 for any number of lists: up to four fill the row as wide cards, more go into a row that
 * shows a part of the next card so it reads as one to scroll.
 */
@Composable
private fun PlaylistShelf(nas: NasConnection, lists: List<Collection>, loading: Boolean, short: Boolean, heading: androidx.compose.ui.unit.TextUnit, modifier: Modifier,
                          focus: FocusRequester, restore: Int?, keep: (Int) -> Unit, open: (Collection) -> Unit) {
    val playlists = lists.count { it.source == "playlist" }
    Column(modifier, verticalArrangement = Arrangement.spacedBy(10.dp)) {
        ShelfHeading("我的歌单", if (playlists > 0) "$playlists 个歌单 · 收藏排在最前" else "收藏排在最前", heading)
        if (lists.isEmpty()) {
            Text(if (loading) "正在读取…" else "暂无内容", color = Muted, fontSize = 14.sp)
            return@Column
        }
        BoxWithConstraints(Modifier.weight(1f).fillMaxWidth()) {
            val gap = if (short) 9.dp else 13.dp
            val at = restore?.coerceIn(lists.indices) ?: 0
            val half = maxWidth * .5f
            if (lists.size <= 4) {
                Row(Modifier.fillMaxSize().focusRestorer(), horizontalArrangement = Arrangement.spacedBy(gap)) {
                    lists.forEachIndexed { i, item ->
                        ListCard(nas, item, i, wide = true, short = short, modifier = Modifier.weight(1f).fillMaxHeight().widthIn(max = half)
                            .then(if (i == at) Modifier.focusRequester(focus) else Modifier).onFocusChanged { if (it.isFocused) keep(i) }) { open(item) }
                    }
                    if (lists.size == 1) Box(Modifier.weight(1.3f).fillMaxHeight().clip(RoundedCornerShape(13.dp)).border(1.dp, Color.White.copy(alpha = .10f), RoundedCornerShape(13.dp))
                        .padding(16.dp), contentAlignment = Alignment.CenterStart) {
                        Text("在飞牛音乐里建的歌单会出现在这里；只用音乐文件夹时没有歌单。", color = Muted, fontSize = 14.sp)
                    }
                }
            } else {
                val row = rememberLazyListState(initialFirstVisibleItemIndex = at)
                val settle = rememberCoroutineScope()
                val width = (maxWidth - gap * 5) / 5.4f
                LazyRow(Modifier.fillMaxSize().focusRestorer().focusGroup(), state = row, horizontalArrangement = Arrangement.spacedBy(gap)) {
                    itemsIndexed(lists, key = { _, it -> "${it.source}/${it.id}" }) { i, item ->
                        ListCard(nas, item, i, wide = false, short = short, modifier = Modifier.width(width).fillMaxHeight()
                            .then(if (i == at) Modifier.focusRequester(focus) else Modifier).onFocusChanged { if (it.isFocused) { keep(i); if (i == 0) settle.launch { row.animateScrollToItem(0) } } }) { open(item) }
                    }
                }
            }
        }
    }
}

@Composable
private fun ListCard(nas: NasConnection, item: Collection, index: Int, wide: Boolean, short: Boolean, modifier: Modifier, open: () -> Unit) {
    val favorite = item.source == "favorites"
    val bitmap = rememberArtwork(nas, item.cover)
    val count = if (item.count > 0) "${item.count} 首" else ""
    StageTile(modifier, onClick = open) {
        if (favorite) Box(Modifier.fillMaxSize()) {
            FavoriteBackdrop(Modifier.fillMaxSize())
            Row(Modifier.fillMaxSize().padding(horizontal = 14.dp), horizontalArrangement = Arrangement.spacedBy(10.dp), verticalAlignment = Alignment.CenterVertically) {
                FavoriteHeart(Modifier.size(if (short) 40.dp else 54.dp))
                Column {
                    Text("我喜欢的音乐", color = Ink, fontSize = if (short) 14.sp else 17.sp, fontWeight = FontWeight.SemiBold, maxLines = 1)
                    Text(count.ifBlank { "还没有收藏" }, color = Ink.copy(alpha = .75f), fontSize = 12.sp)
                }
            }
        } else if (wide) Row(Modifier.fillMaxSize().padding(8.dp), horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.CenterVertically) {
            ListArt(item, bitmap, index, Modifier.fillMaxHeight().aspectRatio(1f).clip(RoundedCornerShape(9.dp)))
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
                Text(item.title, color = Ink, fontSize = if (short) 14.sp else 17.sp, fontWeight = FontWeight.SemiBold, maxLines = 2, overflow = TextOverflow.Ellipsis)
                if (count.isNotEmpty()) Text(count, color = Muted, fontSize = 12.sp)
            }
        } else Box(Modifier.fillMaxSize()) {
            ListArt(item, bitmap, index, Modifier.fillMaxSize())
            val base = remember(bitmap, item.title) { lerp(Color(0xFF1D2937), coverTone(bitmap, item.title), .21f) }
            Box(Modifier.fillMaxSize().background(Brush.verticalGradient(0f to Color.Transparent, .45f to Color.Transparent, 1f to base.copy(alpha = .96f))))
            Column(Modifier.align(Alignment.BottomStart).padding(horizontal = 9.dp, vertical = 7.dp)) {
                Text(item.title, color = Ink, fontSize = if (short) 12.sp else 14.sp, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                if (count.isNotEmpty()) Text(count, color = Ink.copy(alpha = .70f), fontSize = 11.sp)
            }
        }
    }
}

@Composable
private fun ListArt(item: Collection, bitmap: android.graphics.Bitmap?, index: Int, modifier: Modifier) {
    if (bitmap == null && item.source == "playlist") Image(painterResource(playlistFallbacks[index % playlistFallbacks.size]), null, modifier,
        contentScale = ContentScale.Crop, alignment = Alignment.TopCenter)
    else FerryCover(item.title, bitmap, modifier, radius = 0)
}

// ——— 更多: the page behind an entry, with 换一批 ———

@Composable
private fun DiscoverPage(
    nas: NasConnection, state: TvBrowseState, kind: Discover, busy: Boolean, starting: String?, gutter: Dp, short: Boolean, player: Player,
    play: (List<NasTrack>, Int) -> Unit, playEntry: () -> Unit, open: (Collection) -> Unit, shuffle: () -> Unit, back: () -> Unit,
) {
    val shelf = state.shelf
    val picks = shelf?.picks.orEmpty()
    val items = state.entries(kind)
    val first = remember { FocusRequester() }
    val playFocus = remember { FocusRequester() }
    val restore = remember(shelf) { state.discoverFocus.coerceIn(0, maxOf(0, (if (kind == Discover.Picks) picks.size else items.size) - 1)) }
    val current = rememberPlayback(player, withItems = false)
    val grid = rememberLazyGridState()
    LaunchedEffect(shelf) {
        withFrameNanos {}
        if (shelf == null || (if (kind == Discover.Picks) picks.isEmpty() else items.isEmpty())) runCatching { playFocus.requestFocus() }
        else { grid.scrollToItem(restore); withFrameNanos {}; runCatching { first.requestFocus() }.onFailure { runCatching { playFocus.requestFocus() } } }
    }
    Column(Modifier.fillMaxSize().padding(horizontal = gutter), verticalArrangement = Arrangement.spacedBy(if (short) 8.dp else 14.dp)) {
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text(kind.title, color = Ink, fontFamily = FontFamily.SansSerif, fontSize = if (short) 26.sp else 34.sp, fontWeight = FontWeight.SemiBold)
                Text(if (busy && shelf == null) "正在换…" else kind.note, color = Muted, fontSize = 14.sp)
            }
            TvButton(when (kind) { Discover.Picks -> "全部播放"; Discover.Albums -> if (starting == kind.name) "正在取歌…" else "随机放一张"; Discover.Artists -> if (starting == kind.name) "正在取歌…" else "随机放一位" },
                Modifier.focusRequester(playFocus), enabled = shelf != null && starting == null && (if (kind == Discover.Picks) picks.isNotEmpty() else items.isNotEmpty()), primary = true, onClick = playEntry)
            TvButton(if (busy) "正在换…" else "换一批", enabled = !busy, onClick = shuffle)
            TvButton("返回首页", onClick = back)
        }
        if (kind == Discover.Picks) {
            LazyVerticalGrid(GridCells.Fixed(2), Modifier.fillMaxSize().focusRestorer(), state = grid,
                contentPadding = PaddingValues(vertical = 4.dp, horizontal = 2.dp),
                horizontalArrangement = Arrangement.spacedBy(12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                itemsIndexed(picks, key = { i, t -> "${t.id}/$i" }) { i, track ->
                    val here = current.mediaId?.split('/')?.getOrNull(1) == track.id.toString()
                    TvTrackRow(nas, track, Modifier.then(if (i == restore) Modifier.focusRequester(first) else Modifier)
                        .onFocusChanged { if (it.isFocused) state.discoverFocus = i }, index = i + 1, current = here, playing = here && current.playing) { play(picks, i) }
                }
            }
        } else {
            LazyVerticalGrid(GridCells.Fixed(6), Modifier.fillMaxSize().focusRestorer(), state = grid,
                contentPadding = PaddingValues(vertical = 4.dp, horizontal = 2.dp),
                horizontalArrangement = Arrangement.spacedBy(14.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
                itemsIndexed(items, key = { i, c -> "${c.id}/${c.name}/$i" }) { i, item ->
                    CoverCard(nas, item, round = kind == Discover.Artists, Modifier.then(if (i == restore) Modifier.focusRequester(first) else Modifier)
                        .onFocusChanged { if (it.isFocused) state.discoverFocus = i }) { open(item) }
                }
            }
        }
    }
}

@Composable
private fun CoverCard(nas: NasConnection, item: Collection, round: Boolean, modifier: Modifier, open: () -> Unit) {
    val bitmap = rememberArtwork(nas, item.cover)
    var focused by remember { mutableStateOf(false) }
    Surface(onClick = open, modifier = modifier.onFocusChanged { focused = it.isFocused },
        shape = ClickableSurfaceDefaults.shape(RoundedCornerShape(12.dp)),
        colors = ClickableSurfaceDefaults.colors(containerColor = Color.Transparent, focusedContainerColor = Color.White.copy(alpha = .08f)),
        scale = ClickableSurfaceDefaults.scale(focusedScale = 1.04f, pressedScale = .99f)) {
        Column(Modifier.padding(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp), horizontalAlignment = if (round) Alignment.CenterHorizontally else Alignment.Start) {
            val shape = if (round) CircleShape else RoundedCornerShape(10.dp)
            Box(Modifier.fillMaxWidth().aspectRatio(1f).clip(shape).border(if (focused) 2.dp else 1.dp, if (focused) StageGlass else Brush.linearGradient(listOf(Color(0x22FFFFFF), Color(0x22FFFFFF))), shape)) {
                FerryCover(item.title, bitmap, Modifier.fillMaxSize(), radius = 0)
            }
            Text(item.title, color = Ink, fontSize = 14.sp, fontWeight = FontWeight.Medium, maxLines = 1, overflow = TextOverflow.Ellipsis)
            val by = listOf(if (round) "" else item.artist, if (item.count > 0) "${item.count} 首" else "").filter { it.isNotBlank() }.joinToString(" · ")
            if (by.isNotEmpty()) Text(by, color = Muted, fontSize = 11.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    }
}

// ——— The capsule at the bottom of the other pages ———

@Composable
private fun NowCapsule(player: Player, nas: NasConnection, gutter: Dp, open: () -> Unit) {
    val playback = rememberPlayback(player, withItems = false)
    if (playback.mediaId == null) return
    val position = rememberPosition(player, playback, 500)
    val bitmap = rememberArtwork(nas, playback.cover)
    val tone = remember(bitmap, playback.title) { coverTone(bitmap, playback.album.ifBlank { playback.title }) }
    val active = playback.wantsPlay && playback.state != Player.STATE_ENDED
    val shape = RoundedCornerShape(30.dp)
    var focused by remember { mutableStateOf(false) }
    Box(Modifier.fillMaxWidth().padding(horizontal = gutter, vertical = 8.dp), contentAlignment = Alignment.Center) {
        Row(Modifier.widthIn(max = 720.dp).fillMaxWidth(.66f).clip(shape)
            .background(Brush.horizontalGradient(listOf(lerp(Backdrop, tone, .45f).copy(alpha = .94f), Color(0xEE0D151B))))
            .border(.8.dp, Color.White.copy(alpha = .12f), shape)
            .drawWithContent {
                drawContent()
                val f = if (playback.duration > 0) (position.longValue.toFloat() / playback.duration).coerceIn(0f, 1f) else 0f
                val y = size.height - 1.5.dp.toPx()
                val inset = 26.dp.toPx()
                val w = size.width - inset * 2
                drawLine(Color.White.copy(alpha = .10f), Offset(inset, y), Offset(inset + w, y), 2.dp.toPx())
                drawLine(Brush.horizontalGradient(listOf(FishLow, FishHigh), inset, inset + w), Offset(inset, y), Offset(inset + w * f, y), 2.dp.toPx())
            }
            .padding(5.dp), horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
            Surface(onClick = open, modifier = Modifier.weight(1f).onFocusChanged { focused = it.isFocused },
                shape = ClickableSurfaceDefaults.shape(RoundedCornerShape(26.dp)),
                colors = ClickableSurfaceDefaults.colors(containerColor = Color.Transparent, focusedContainerColor = Color.White.copy(alpha = .12f)),
                scale = ClickableSurfaceDefaults.scale(focusedScale = 1f, pressedScale = .99f)) {
                Row(Modifier.fillMaxWidth().padding(start = 4.dp, end = 12.dp, top = 3.dp, bottom = 3.dp), horizontalArrangement = Arrangement.spacedBy(11.dp), verticalAlignment = Alignment.CenterVertically) {
                    FerryCover(playback.title, bitmap, Modifier.size(38.dp).clip(CircleShape), radius = 19)
                    Column(Modifier.weight(1f)) {
                        Text(playback.title, color = Ink, fontSize = 15.sp, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        Text(when {
                            playback.failed -> "播放出错了，按一下去看看"
                            playback.state == Player.STATE_BUFFERING -> "正在缓冲"
                            focused -> "打开播放页"
                            else -> playback.artist.ifBlank { "TrackFerry" }
                        }, color = Muted, fontSize = 12.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    }
                    BassDot(Modifier.size(14.dp), playback.playing)
                }
            }
            GlyphButton(if (active) "暂停" else "播放", if (active) TvGlyph.Pause else TvGlyph.Play, size = 38.dp, glass = true) {
                if (active) player.pause() else {
                    if (playback.failed || playback.state == Player.STATE_IDLE) player.prepare()
                    if (playback.state == Player.STATE_ENDED) player.seekToDefaultPosition()
                    player.play()
                }
            }
            GlyphButton("下一首", TvGlyph.Next, size = 38.dp, enabled = player.hasNextMediaItem()) { player.seekToNextMediaItem() }
        }
    }
}

// ——— A list's songs ———

@Composable
private fun TrackBrowser(
    nas: NasConnection, player: Player, selected: Collection, state: TvBrowseState, busy: Boolean, art: android.graphics.Bitmap?,
    gutter: Dp, start: (List<NasTrack>, Int) -> Unit,
) {
    val tracks = state.tracks
    var currentTrackId by remember(player) { mutableStateOf(player.currentMediaItem?.identity?.toLongOrNull()) }
    var playing by remember(player) { mutableStateOf(player.isPlaying) }
    DisposableEffect(player) {
        val listener = object : Player.Listener {
            override fun onMediaItemTransition(mediaItem: MediaItem?, reason: Int) { currentTrackId = mediaItem?.identity?.toLongOrNull() }
            override fun onIsPlayingChanged(isPlaying: Boolean) { playing = isPlaying }
        }
        player.addListener(listener)
        currentTrackId = player.currentMediaItem?.identity?.toLongOrNull()
        playing = player.isPlaying
        onDispose { player.removeListener(listener) }
    }
    val currentIndex = remember(tracks, currentTrackId) { tracks?.indexOfFirst { it.id == currentTrackId } ?: -1 }
    val back = remember { FocusRequester() }
    val prior = remember { FocusRequester() }
    val list = rememberLazyListState()
    val restore = remember(tracks) { state.trackFocus.coerceIn(0, (tracks?.lastIndex ?: 0).coerceAtLeast(0)) }
    LaunchedEffect(selected) {
        withFrameNanos {}
        if (tracks.isNullOrEmpty()) runCatching { back.requestFocus() }
    }
    LaunchedEffect(tracks) {
        if (!tracks.isNullOrEmpty()) {
            list.scrollToItem(restore)
            withFrameNanos {}
            runCatching { prior.requestFocus() }
        }
    }
    Column(Modifier.fillMaxSize().padding(horizontal = gutter), verticalArrangement = Arrangement.spacedBy(9.dp)) {
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(17.dp), verticalAlignment = Alignment.CenterVertically) {
            FerryCover(selected.title, art, Modifier.size(90.dp))
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(5.dp)) {
                Text(selected.title, color = Ink, fontSize = 32.sp, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(listOf(selected.artist, tracks?.let { "${it.size} 首" }.orEmpty()).filter { it.isNotBlank() }.joinToString("  ·  "), color = Muted, fontSize = 16.sp)
            }
            TvButton(when {
                state.discover != null -> "返回"
                state.section == TvSection.Mine -> "返回设置"
                state.section == TvSection.Search -> "返回搜索"
                else -> "返回首页"
            }, Modifier.focusRequester(back), onClick = state::closeCollection)
            TvButton("播放全部", enabled = !tracks.isNullOrEmpty(), primary = true) { start(tracks.orEmpty(), 0) }
        }
        if (busy) Text("正在读取…", color = Muted, fontSize = 17.sp)
        if (tracks?.isEmpty() == true && !busy) Text("没有可播放的歌曲", color = Muted, fontSize = 17.sp)
        LazyColumn(state = list, modifier = Modifier.focusRestorer(), contentPadding = PaddingValues(vertical = 9.dp, horizontal = 6.dp), verticalArrangement = Arrangement.spacedBy(7.dp)) {
            itemsIndexed(tracks.orEmpty(), key = { i, track -> "${track.id}/$i" }) { i, track ->
                TvTrackRow(nas, track, Modifier.then(if (i == restore) Modifier.focusRequester(prior) else Modifier).onFocusChanged {
                    if (it.isFocused) state.trackFocus = i
                }, index = i + 1, current = i == currentIndex, playing = i == currentIndex && playing) { state.trackFocus = i; start(tracks.orEmpty(), i) }
            }
        }
    }
}

@Composable
internal fun TvTrackRow(nas: NasConnection, track: NasTrack, modifier: Modifier = Modifier, index: Int? = null,
                        current: Boolean = false, playing: Boolean = false, onClick: () -> Unit) {
    val bitmap = rememberArtwork(nas, track.cover)
    FerryTile(modifier.fillMaxWidth(), onClick = onClick) {
        Row(Modifier.padding(horizontal = 12.dp, vertical = 7.dp), horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.CenterVertically) {
            if (index != null) Text(index.toString(), Modifier.width(28.dp), color = Muted, fontSize = 14.sp)
            FerryCover(track.title, bitmap, Modifier.size(40.dp), radius = 7)
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                TrackTitle(track.title, current, playing, 17.sp, FontWeight.Medium)
                val subtitle = listOf(track.artist, track.album).filter { it.isNotBlank() }.joinToString(" · ")
                if (subtitle.isNotEmpty()) Text(subtitle, color = Muted, fontSize = 13.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            if (track.duration > 0) Text(clock(track.duration), color = Muted, fontSize = 13.sp)
            if (index == null) StageIcon(TvGlyph.Play, Modifier.size(15.dp), Teal)
        }
    }
}
