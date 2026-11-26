package com.trackferry.tv

import android.net.Uri
import androidx.media3.common.MediaItem
import androidx.media3.common.MediaMetadata

internal data class DemoTrack(
    val id: String,
    val title: String,
    val audio: Int,
    val lyrics: Int,
) {
    fun mediaItem(packageName: String): MediaItem = MediaItem.Builder()
        .setMediaId(id)
        .setUri(Uri.parse("android.resource://$packageName/$audio"))
        .setMediaMetadata(
            MediaMetadata.Builder()
                .setTitle(title)
                .setArtist("TrackFerry")
                .setAlbumTitle("播放验证")
                .build(),
        )
        .build()
}

internal val Tracks = listOf(
    DemoTrack("tide", "潮汐来信", R.raw.tide, R.raw.tide_lrc),
    DemoTrack("moon", "月光回航", R.raw.moon, R.raw.moon_lrc),
)
