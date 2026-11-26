package com.trackferry.tv

import androidx.media3.common.AudioAttributes
import androidx.media3.common.C
import androidx.media3.common.Player
import androidx.media3.common.MediaItem
import androidx.media3.datasource.DefaultDataSource
import androidx.media3.datasource.okhttp.OkHttpDataSource
import androidx.media3.exoplayer.source.DefaultMediaSourceFactory
import com.google.common.util.concurrent.Futures
import com.google.common.util.concurrent.ListenableFuture
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import androidx.media3.common.util.UnstableApi
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.session.MediaSession
import androidx.media3.session.MediaSessionService

@androidx.annotation.OptIn(UnstableApi::class)
class PlaybackService : MediaSessionService() {
    companion object {
        // Activity and service share a process. Large catalogs stay out of Binder bundles.
        internal var playCatalog: ((List<NasTrack>, Int) -> Boolean)? = null
            private set
        internal var stopPlayback: (() -> Unit)? = null
            private set
    }

    private var session: MediaSession? = null
    private var remote: TvRemote? = null
    private var drift: Job? = null
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)

    override fun onCreate() {
        super.onCreate()
        val sources = DefaultDataSource.Factory(this, OkHttpDataSource.Factory(NasPlayback.calls))
        SoundSettings.load(this)
        val player = ExoPlayer.Builder(this, SoundRenderers(this))
            .setMediaSourceFactory(DefaultMediaSourceFactory(this).setDataSourceFactory(sources))
            .setAudioAttributes(AudioAttributes.DEFAULT, true)
            .setHandleAudioBecomingNoisy(true)
            .setWakeMode(C.WAKE_MODE_LOCAL)
            .build()
        remote = TvRemote(applicationContext,player,scope).also { it.begin() }
        session = MediaSession.Builder(this, player).setCallback(Controls()).build()
        stopPlayback = { player.pause(); player.stop() }
        playCatalog = { tracks, index ->
            val nas = NasPlayback.connection
            if (nas == null || tracks.size > MAX_QUEUE || index !in tracks.indices) false
            else {
                val media = tracks.map { it.mediaItem(true) }
                player.setMediaItems(media, index, 0)
                player.prepare()
                player.play()
                drift?.cancel()
                // A song put on alone goes on with random songs, as in the browser, not into silence.
                if (media.size == 1) drift = scope.launch { goOn(player, nas, media.single().mediaId, tracks.single()) }
                true
            }
        }
    }

    /** Adds the songs after a lone one; tries again a few times so a slow server does not end the music. */
    private suspend fun goOn(player: ExoPlayer, nas: NasConnection, entry: String, alone: NasTrack) {
        for (attempt in 0 until 4) {
            if (player.mediaItemCount != 1 || player.getMediaItemAt(0).mediaId != entry || NasPlayback.connection !== nas) return
            try {
                val more = withContext(Dispatchers.IO) { nas.drift() }.filter { it.id != alone.id || alone.online.isNotEmpty() }
                if (player.mediaItemCount != 1 || player.getMediaItemAt(0).mediaId != entry) return
                if (more.isNotEmpty()) { player.addMediaItems(more.map { it.mediaItem(true) }); return }
            } catch (e: CancellationException) { throw e }
            catch (_: Exception) { /* tried again below */ }
            delay(3_000L * (attempt + 1))
        }
    }

    override fun onGetSession(controllerInfo: MediaSession.ControllerInfo) = session

    override fun onDestroy() {
        playCatalog = null
        stopPlayback = null
        drift?.cancel()
        remote?.close()
        scope.cancel()
        session?.let {
            it.player.release()
            it.release()
        }
        session = null
        super.onDestroy()
    }

    private inner class Controls : MediaSession.Callback {
        override fun onAddMediaItems(
            session: MediaSession,
            controller: MediaSession.ControllerInfo,
            mediaItems: MutableList<MediaItem>,
        ): ListenableFuture<MutableList<MediaItem>> {
            val items = mediaItems.mapNotNull { item ->
                val local = Tracks.firstOrNull { it.id == item.mediaId }
                val track = item.identity
                when {
                    local != null -> local.mediaItem(packageName)
                    track != null -> item.buildUpon().setUri(mediaUri(track,item.mediaMetadata.title?.toString().orEmpty(),item.mediaMetadata.artist?.toString().orEmpty())).build()
                    else -> null
                }
            }
            if (items.size != mediaItems.size) return Futures.immediateFailedFuture(IllegalArgumentException("Unknown track"))
            return Futures.immediateFuture(items.toMutableList())
        }

        override fun onConnect(
            session: MediaSession,
            controller: MediaSession.ControllerInfo,
        ): MediaSession.ConnectionResult {
            if (controller.packageName == packageName) {
                return super.onConnect(session, controller)
            }
            if (!controller.isTrusted) return MediaSession.ConnectionResult.reject()
            val commands = MediaSession.ConnectionResult.DEFAULT_PLAYER_COMMANDS.buildUpon()
                .remove(Player.COMMAND_CHANGE_MEDIA_ITEMS)
                .remove(Player.COMMAND_SET_MEDIA_ITEM)
                .build()
            return MediaSession.ConnectionResult.AcceptedResultBuilder(session)
                .setAvailablePlayerCommands(commands)
                .build()
        }
    }
}
