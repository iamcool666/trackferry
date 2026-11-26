package com.trackferry.tv

import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.grid.itemsIndexed
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.rememberLazyGridState
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.ui.focus.focusRestorer
import androidx.compose.ui.graphics.SolidColor
import androidx.tv.material3.ClickableSurfaceDefaults
import androidx.tv.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.platform.LocalSoftwareKeyboardController
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.tv.material3.Text
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

internal enum class TvSearchSource(val label: String) { Library("飞牛曲库"), Online("在线音源") }
internal data class TvSearchRequest(val text: String, val source: TvSearchSource)

internal class TvSearchState {
    var query by mutableStateOf("")
    var source by mutableStateOf(TvSearchSource.Library)
    var request by mutableStateOf<TvSearchRequest?>(null)
    var loaded: TvSearchRequest? = null
    var results by mutableStateOf(SearchResults(emptyList()))
    var recent by mutableStateOf(emptyList<TvSearchRequest>())
    var focused by mutableStateOf(0)
}

@Composable
internal fun TvSearchScreen(nas: NasConnection, state: TvSearchState, gutter: Dp, short: Boolean,
                            revoked: () -> Unit, open: (Collection) -> Unit, playOne: (NasTrack) -> Unit) {
    var retry by remember { mutableStateOf(0) }
    var busy by remember { mutableStateOf(state.request != null && state.loaded != state.request) }
    var error by remember { mutableStateOf("") }
    val keyboard = LocalSoftwareKeyboardController.current
    val resultFocus = remember { FocusRequester() }
    val inputFocus = remember { FocusRequester() }
    val grid = rememberLazyGridState()
    val request = state.request
    val result = state.results.takeIf { state.loaded == request && request != null }
    val results = result?.tracks.orEmpty()
    fun submit(text: String = state.query, source: TvSearchSource = state.source) {
        val term = text.trim()
        if (term.isEmpty()) return
        keyboard?.hide()
        state.query = term
        state.source = source
        state.focused = 0
        state.loaded = null
        state.request = TvSearchRequest(term, source)
        retry++
    }
    fun clear() {
        state.query = ""
        state.request = null
        state.loaded = null
        state.focused = 0
        error = ""
        busy = false
        runCatching { inputFocus.requestFocus() }
    }
    LaunchedEffect(Unit) {
        withFrameNanos {}
        if (request == null) runCatching { inputFocus.requestFocus() }
    }
    LaunchedEffect(nas, request, retry) {
        if (request == null || request == state.loaded) return@LaunchedEffect
        busy = true
        error = ""
        try {
            val matches = withContext(Dispatchers.IO) { nas.search(request.text, request.source == TvSearchSource.Online) }
            state.results = matches
            state.loaded = request
            state.recent = (listOf(request) + state.recent.filterNot { it == request }).take(6)
        } catch (e: CancellationException) { throw e }
        catch (e: Exception) {
            if (e is TvHttpError && e.status == 401) revoked()
            else error = e.tvMessage(if (request.source == TvSearchSource.Online) "暂时无法搜索在线音源" else "暂时无法搜索飞牛曲库")
        } finally { busy = false }
    }
    val collections = remember(request, results) {
        if (request?.source != TvSearchSource.Library) emptyList() else searchCollections(request.text, results)
    }
    val artists = collections.filter { it.source == "artist" }
    val albums = collections.filter { it.source == "album" }
    // Songs come after the full-width rows (a heading and a row for each kind, and the songs heading).
    val lead = (if (artists.isNotEmpty()) 2 else 0) + (if (albums.isNotEmpty()) 2 else 0) + 1
    val landing = remember(request, retry, results) { state.focused.coerceIn(0, (results.size - 1).coerceAtLeast(0)) }
    LaunchedEffect(request, retry, results) {
        if (results.getOrNull(landing) == null) return@LaunchedEffect
        grid.scrollToItem(if (landing == 0) 0 else lead + landing)
        withFrameNanos {}
        runCatching { resultFocus.requestFocus() }
    }
    val gap = if (short) 8.dp else 12.dp
    Column(Modifier.fillMaxSize().padding(horizontal = gutter), verticalArrangement = Arrangement.spacedBy(gap)) {
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(10.dp), verticalAlignment = Alignment.CenterVertically) {
            StagePanel(Modifier.weight(1f), padding = 5.dp) {
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
                    Box(Modifier.weight(1f)) {
                        TvInput(state.query, { state.query = it.take(100) }, "歌名、歌手、专辑", Modifier.focusRequester(inputFocus), glyph = TvGlyph.Search, inset = false) { submit() }
                    }
                    if (state.query.isNotEmpty()) GlyphButton("清空搜索", TvGlyph.Close) { clear() }
                }
            }
            StagePanel(padding = 3.dp) {
                Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                    TvSearchSource.entries.forEach { source ->
                        StageNav(source.label, if (source == TvSearchSource.Library) TvGlyph.Queue else TvGlyph.Discover, selected = source == state.source) {
                            if (state.source != source) {
                                state.source = source
                                if (state.query.isNotBlank() && request != null) submit(source = source)
                            } else if (state.query.isNotBlank() && !busy) submit()
                        }
                    }
                }
            }
        }
        when {
            request == null -> SearchLanding(state.recent) { submit(it.text, it.source) }
            error.isNotEmpty() -> Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text(error, color = Accent, fontSize = 16.sp)
                TvButton("再搜一次", primary = true) { submit(request.text, request.source) }
            }
            busy || result == null -> Text("正在${if (request.source == TvSearchSource.Online) "在线源" else "曲库"}里找「${request.text}」…", color = Muted, fontSize = 16.sp)
            results.isEmpty() -> Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text(if (request.source == TvSearchSource.Library) "曲库里没有找到「${request.text}」" else "在线源也没有找到「${request.text}」", color = Ink, fontSize = 20.sp)
                if (request.source == TvSearchSource.Library) TvButton("去在线源找", primary = true) { submit(request.text, TvSearchSource.Online) }
            }
            else -> LazyVerticalGrid(GridCells.Fixed(2), Modifier.fillMaxSize().focusRestorer(), state = grid,
                contentPadding = PaddingValues(vertical = 4.dp, horizontal = 2.dp),
                horizontalArrangement = Arrangement.spacedBy(14.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                if (artists.isNotEmpty()) {
                    item(span = { GridItemSpan(maxLineSpan) }) { SearchHeading("歌手") }
                    item(span = { GridItemSpan(maxLineSpan) }) {
                        LazyRow(horizontalArrangement = Arrangement.spacedBy(16.dp), contentPadding = PaddingValues(vertical = 4.dp)) {
                            items(artists, key = { "a/${it.name}" }) { SearchCollection(nas, it, round = true, short = short) { open(it) } }
                        }
                    }
                }
                if (albums.isNotEmpty()) {
                    item(span = { GridItemSpan(maxLineSpan) }) { SearchHeading("专辑") }
                    item(span = { GridItemSpan(maxLineSpan) }) {
                        LazyRow(horizontalArrangement = Arrangement.spacedBy(16.dp), contentPadding = PaddingValues(vertical = 4.dp)) {
                            items(albums, key = { "b/${it.id}" }) { SearchCollection(nas, it, round = false, short = short) { open(it) } }
                        }
                    }
                }
                item(span = { GridItemSpan(maxLineSpan) }) {
                    val count = if (results.size == 50 && request.source == TvSearchSource.Library) "50+" else results.size.toString()
                    val unavailable = result.unavailable
                    SearchHeading("歌曲  $count", if (unavailable.isNotEmpty()) "${unavailable.joinToString("、", transform = ::tvSourceName)}这次没有回应" else "")
                }
                itemsIndexed(results, key = { index, track -> "${track.online}/${track.id}/$index" }) { index, track ->
                    SearchHit(nas, track, Modifier.then(if (index == landing) Modifier.focusRequester(resultFocus) else Modifier)
                        .onFocusChanged { if (it.isFocused) state.focused = index }) {
                        if (!track.restricted) { state.focused = index; playOne(track) }
                    }
                }
            }
        }
    }
}

@Composable
private fun SearchHeading(title: String, note: String = "") {
    Row(Modifier.padding(top = 6.dp, bottom = 2.dp), horizontalArrangement = Arrangement.spacedBy(10.dp), verticalAlignment = Alignment.Bottom) {
        Text(title, color = Ink, fontSize = 18.sp, fontWeight = FontWeight.SemiBold)
        if (note.isNotEmpty()) Text(note, color = Muted, fontSize = 12.sp)
    }
}

@Composable
private fun SearchLanding(recent: List<TvSearchRequest>, again: (TvSearchRequest) -> Unit) {
    Column(Modifier.fillMaxWidth().padding(top = 6.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
        Text("按确认键打开输入法；曲库里没有的歌，可以切到在线音源。", color = Muted, fontSize = 15.sp)
        if (recent.isNotEmpty()) {
            Text("最近搜过", color = Ink, fontSize = 17.sp, fontWeight = FontWeight.SemiBold)
            LazyRow(horizontalArrangement = Arrangement.spacedBy(9.dp)) {
                items(recent, key = { "${it.source}/${it.text}" }) { entry ->
                    StageNav("${entry.text.take(16)}${if (entry.source == TvSearchSource.Online) " · 在线" else ""}", TvGlyph.Search) { again(entry) }
                }
            }
        }
    }
}

@Composable
private fun SearchCollection(nas: NasConnection, item: Collection, round: Boolean, short: Boolean, open: () -> Unit) {
    val bitmap = rememberArtwork(nas, item.cover)
    var focused by remember { mutableStateOf(false) }
    val side = if (short) 84.dp else 112.dp
    Surface(onClick = open, modifier = Modifier.width(side + 12.dp).onFocusChanged { focused = it.isFocused },
        shape = ClickableSurfaceDefaults.shape(RoundedCornerShape(12.dp)),
        colors = ClickableSurfaceDefaults.colors(containerColor = Color.Transparent, focusedContainerColor = Color.White.copy(alpha = .08f)),
        scale = ClickableSurfaceDefaults.scale(focusedScale = 1.05f, pressedScale = .99f)) {
        Column(Modifier.padding(6.dp), verticalArrangement = Arrangement.spacedBy(5.dp), horizontalAlignment = Alignment.CenterHorizontally) {
            val shape = if (round) CircleShape else RoundedCornerShape(10.dp)
            Box(Modifier.size(side).clip(shape).border(if (focused) 2.dp else 1.dp, if (focused) StageGlass else SolidColor(Color(0x22FFFFFF)), shape)) {
                FerryCover(item.title, bitmap, Modifier.fillMaxSize(), radius = 0)
            }
            Text(item.title, color = Ink, fontSize = 13.sp, fontWeight = FontWeight.Medium, maxLines = 1, overflow = TextOverflow.Ellipsis)
            if (!round && item.artist.isNotBlank()) Text(item.artist, color = Muted, fontSize = 11.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    }
}

private fun searchCollections(query: String, tracks: List<NasTrack>): List<Collection> {
    fun String.normalized() = lowercase().filterNot(Char::isWhitespace)
    val term = query.normalized()
    val artists = tracks.flatMap { t -> t.artist.split(Regex("[、/;,&]")).map { it.trim() }
        .filter { it.isNotBlank() && it.normalized().contains(term) }.map { Collection(it, "artist", name = it, cover = t.cover) } }
        .distinctBy { it.title.normalized() }.take(3)
    val albums = tracks.filter { it.albumId.isNotBlank() && it.album.normalized().contains(term) }
        .distinctBy { it.albumId }.take(4).map { Collection(it.album, "album", it.albumId, artist = it.artist, cover = it.cover) }
    return artists + albums
}

internal fun tvSourceName(source: String) = when (source) {
    "kuwo" -> "酷我"
    "kugou" -> "酷狗"
    "migu" -> "咪咕"
    "netease" -> "网易云"
    "" -> "飞牛曲库"
    else -> source
}

@Composable
private fun ResultArtwork(nas: NasConnection, track: NasTrack, modifier: Modifier) {
    val provider = when (track.source) {
        "kuwo" -> R.drawable.source_kuwo
        "kugou" -> R.drawable.source_kugou
        "migu" -> R.drawable.source_migu
        "netease" -> R.drawable.source_netease
        else -> null
    }
    if (provider != null) Image(painterResource(provider), null, modifier.clip(RoundedCornerShape(8.dp)), contentScale = ContentScale.Crop)
    else FerryCover(track.title, rememberArtwork(nas, track.cover), modifier, radius = 8)
}

@Composable
private fun SearchHit(nas: NasConnection, track: NasTrack, modifier: Modifier, open: () -> Unit) {
    Column(Modifier.fillMaxWidth()) {
        StageTile(modifier.fillMaxWidth(), quiet = true, onClick = open) {
            Row(Modifier.fillMaxWidth().padding(horizontal = 8.dp, vertical = 5.dp), horizontalArrangement = Arrangement.spacedBy(10.dp), verticalAlignment = Alignment.CenterVertically) {
                ResultArtwork(nas, track, Modifier.size(36.dp))
                Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(1.dp)) {
                    Text(track.title, color = Ink.copy(alpha = if (track.restricted) .68f else 1f), fontSize = 16.sp, fontWeight = FontWeight.Medium, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    Text(listOf(track.artist.ifBlank { "没有歌手信息" }, track.album, tvSourceName(track.source), if (track.restricted) "要会员" else track.note)
                        .filter { it.isNotBlank() }.joinToString(" · "), color = Muted, fontSize = 12.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
                if (track.duration > 0) Text(clock(track.duration), color = Muted, fontSize = 12.sp)
                StageIcon(if (track.restricted) TvGlyph.Lock else TvGlyph.Play, Modifier.size(14.dp), if (track.restricted) Accent else Teal)
            }
        }
        Box(Modifier.fillMaxWidth().padding(horizontal = 8.dp).height(1.dp).background(Color.White.copy(alpha = .08f)))
    }
}
