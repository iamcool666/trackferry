package com.trackferry.tv

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.tv.material3.Text
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

internal data class PlaylistSong(val id: Long, val title: String)

@Composable
internal fun TvPlaylistPicker(nas: NasConnection, song: PlaylistSong, onResult: (String) -> Unit) {
    var lists by remember(nas) { mutableStateOf<List<Collection>?>(null) }
    var error by remember(nas) { mutableStateOf("") }
    var saving by remember { mutableStateOf(false) }
    var retry by remember { mutableStateOf(0) }
    val scope = rememberCoroutineScope()
    LaunchedEffect(nas, retry) {
        error = ""
        try { lists = withContext(Dispatchers.IO) { nas.playlists() } }
        catch (e: CancellationException) { throw e }
        catch (e: Exception) { error = e.tvMessage("无法读取飞牛歌单") }
    }
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text("加入歌单", color = Ink, fontSize = 32.sp)
        Text(song.title, color = Muted, fontSize = 16.sp)
        if (error.isNotBlank()) Text(error, color = Accent, fontSize = 15.sp)
        when (val loaded = lists) {
            null -> if (error.isBlank()) Text("正在读取歌单…", color = Muted) else TvButton("重试") { retry++ }
            emptyList<Collection>() -> Text("飞牛音乐暂无歌单", color = Muted)
            else -> LazyColumn(contentPadding = PaddingValues(4.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                items(loaded, key = { it.id }) { item ->
                    FerryTile(Modifier.fillMaxWidth(), enabled = !saving, onClick = {
                        if (!saving) scope.launch {
                            saving = true
                            error = ""
                            try {
                                val changed = withContext(Dispatchers.IO) { nas.addToPlaylist(item.id, song.id) }
                                onResult(if (changed) "已加入「${item.title}」" else "歌曲已在「${item.title}」中")
                            } catch (e: CancellationException) { throw e }
                            catch (e: Exception) { error = e.tvMessage("无法加入歌单") }
                            finally { saving = false }
                        }
                    }) { Text(item.title, Modifier.padding(12.dp), color = Ink, fontSize = 18.sp) }
                }
            }
        }
    }
}
