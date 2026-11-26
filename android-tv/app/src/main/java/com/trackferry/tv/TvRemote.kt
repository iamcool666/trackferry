package com.trackferry.tv

import android.content.Context
import android.net.Uri
import android.os.Bundle
import android.os.SystemClock
import androidx.media3.common.MediaItem
import androidx.media3.common.MediaMetadata
import androidx.media3.common.Player
import androidx.media3.common.PlaybackException
import androidx.media3.exoplayer.ExoPlayer
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import org.json.JSONArray
import org.json.JSONObject
import java.util.UUID

internal const val MAX_QUEUE = 20_000
private fun String.refKey() = toLongOrNull()?.let { JSONObject().put("id", it) } ?: JSONObject().put("online", this)
internal val MediaItem.identity: String? get() = mediaId.takeIf { it.startsWith("nas/") }?.split('/')?.getOrNull(1)

internal fun NasTrack.mediaItem(withUri: Boolean): MediaItem {
    val key = online.ifBlank { id.toString() }
    val media = MediaItem.Builder().setMediaId("nas/$key/${UUID.randomUUID()}")
        .setMediaMetadata(MediaMetadata.Builder().setTitle(title).setArtist(artist).setAlbumTitle(album)
            .setExtras(Bundle().apply { putString("cover", cover) }).build())
    if (withUri) media.setUri(mediaUri(key, title, artist))
    return media.build()
}

internal fun mediaUri(ref: String, title: String, artist: String): Uri = Uri.parse(MEDIA_ORIGIN).buildUpon().apply {
    if (ref.toLongOrNull() != null) appendQueryParameter("track", ref)
    else {
        appendQueryParameter("online", ref)
        appendQueryParameter("title", title)
        appendQueryParameter("artist", artist)
    }
}.build()

internal fun Player.modeName() = if (repeatMode == Player.REPEAT_MODE_ONE) "one" else if (shuffleModeEnabled) "shuffle" else "loop"
internal fun Player.setMode(name: String) {
    if (name !in listOf("loop", "one", "shuffle")) return
    repeatMode = if (name == "one") Player.REPEAT_MODE_ONE else Player.REPEAT_MODE_ALL
    shuffleModeEnabled = name == "shuffle"
}

/** Owns the local snapshot and the remote command channel of the same ExoPlayer. */
internal class TvRemote(private val context: Context, private val player: ExoPlayer, private val scope: CoroutineScope) : Player.Listener {
    private val disk = context.getSharedPreferences("tv_playback", Context.MODE_PRIVATE)
    private var link: NasConnection? = null
    private var epoch = UUID.randomUUID().toString()
    private var sequence = 0L
    private var revision = UUID.randomUUID().toString()
    private var sentRevision = ""
    private var keys = emptyList<String>()
    private var queue = JSONArray()
    private var handled = ""
    private var receipt: JSONObject? = null
    private var pending: Waiting? = null
    private var awaitingLoadReceipt: Waiting? = null
    private var previousWrite = 0L
    private var writtenQueue = ""
    private var loop: Job? = null
    private var reconnect: Job? = null
    private var failures = 0
    private var retries = 0
    private var retryAt = 0L

    private data class Waiting(val id: String, val entry: String, val action: String, val until: Long, val position: Long, var firstPosition: Long? = null)

    fun begin() {
        player.volume = disk.getFloat("volume", 1f)
        player.addListener(this)
        loop = scope.launch {
            while (true) {
                val nas = NasPlayback.connection
                if (nas !== link) bind(nas)
                if (nas != null && (NasPlayback.visible || player.playWhenReady && player.playbackState != Player.STATE_ENDED && player.playerError == null)) {
                    report(nas)
                }
                save(false)
                delay(if (pending != null) 230L else if (NasPlayback.visible || player.isPlaying) 550L else 850L)
            }
        }
    }

    fun close() {
        save(true)
        loop?.cancel()
        reconnect?.cancel()
        player.removeListener(this)
    }

    private fun bind(nas: NasConnection?) {
        val old = link
        link = nas
        if (old?.link?.server?.pin == nas?.link?.server?.pin && old?.link?.token == nas?.link?.token) return
        epoch = UUID.randomUUID().toString(); sequence = 0; sentRevision = ""; writtenQueue = ""; handled = ""; receipt = null; pending = null
        awaitingLoadReceipt = null
        reconnect?.cancel(); failures = 0
        if (nas == null) return
        if (player.mediaItemCount == 0) restore(nas)
    }

    override fun onEvents(player: Player, events: Player.Events) {
        if (events.contains(Player.EVENT_VOLUME_CHANGED)) disk.edit().putFloat("volume", player.volume).apply()
        if (events.contains(Player.EVENT_TIMELINE_CHANGED)) {
            val entries = List(player.mediaItemCount, player::getMediaItemAt)
            val ids = entries.map { it.mediaId }
            if (ids != keys) {
                keys = ids
                revision = UUID.randomUUID().toString()
                queue = JSONArray(entries.map { it.reference(false) ?: JSONObject() })
            }
        }
        if (events.contains(Player.EVENT_MEDIA_ITEM_TRANSITION) || events.contains(Player.EVENT_POSITION_DISCONTINUITY) ||
            events.contains(Player.EVENT_PLAY_WHEN_READY_CHANGED) || events.contains(Player.EVENT_TIMELINE_CHANGED) ||
            events.contains(Player.EVENT_REPEAT_MODE_CHANGED) || events.contains(Player.EVENT_SHUFFLE_MODE_ENABLED_CHANGED)) save(true)
    }

    private fun MediaItem.reference(details: Boolean): JSONObject? {
        val ref = identity ?: return null
        val out = ref.refKey()
        if (details || ref.toLongOrNull() == null) out.put("title", mediaMetadata.title?.toString().orEmpty().take(100))
            .put("artist", mediaMetadata.artist?.toString().orEmpty().take(100))
        return out
    }

    private fun snapshot(include: Boolean): JSONObject {
        val current = player.currentMediaItem
        val isDemo = current != null && current.identity == null
        val count = if (isDemo) 0 else player.mediaItemCount
        val phase = when {
            count == 0 -> "idle"
            player.playerError != null -> "error"
            player.isPlaying -> "playing"
            player.playbackState == Player.STATE_BUFFERING -> "buffering"
            player.playbackState == Player.STATE_ENDED -> "ended"
            else -> "paused"
        }
        val out = JSONObject().put("revision", revision).put("entry", current?.takeUnless { isDemo }?.mediaId.orEmpty())
            .put("current", current?.reference(true) ?: JSONObject())
            .put("index", if (count == 0) 0 else player.currentMediaItemIndex).put("count", count)
            .put("position_ms", if (count == 0) 0 else player.currentPosition.coerceAtLeast(0))
            .put("duration_ms", if (count == 0) 0 else player.duration.coerceAtLeast(0))
            .put("phase", phase).put("mode", player.modeName()).put("seekable", !isDemo && player.isCurrentMediaItemSeekable)
            .put("volume", (player.volume * 100).toInt())
        if (include) out.put("queue", if (isDemo) JSONArray() else queue)
        return out
    }

    private fun save(force: Boolean) {
        val nas = link ?: return
        val current = player.currentMediaItem ?: return
        if (current.identity == null) return
        val now = SystemClock.elapsedRealtime()
        if (!force && (!player.isPlaying || now - previousWrite < 5_000)) return
        val state = snapshot(false)
        if (state.optInt("count") !in 1..MAX_QUEUE) return
        previousWrite = now
        val key = nas.link.server.pin
        val edit = disk.edit().putString(key, state.toString())
        if (writtenQueue != revision) {
            writtenQueue = revision
            edit.putString("$key:queue", queue.toString())
        }
        edit.apply()
    }

    private fun restore(nas: NasConnection) {
        val data = runCatching { JSONObject(disk.getString(nas.link.server.pin, null) ?: return) }.getOrNull() ?: return
        val refs = disk.getString("${nas.link.server.pin}:queue", null)?.let { runCatching { JSONArray(it) }.getOrNull() }
            ?: data.optJSONArray("queue") ?: return
        if (refs.length() !in 1..MAX_QUEUE) return
        scope.launch {
            try {
                val items = withContext(Dispatchers.IO) { nas.resolve(refs) }
                if (link !== nas || player.mediaItemCount != 0 || items.isEmpty()) return@launch
                player.setMediaItems(items.map { it.mediaItem(true) }, data.optInt("index").coerceIn(items.indices), data.optLong("position_ms").coerceAtLeast(0))
                player.setMode(data.optString("mode"))
                player.prepare()
            } catch (e: CancellationException) { throw e }
            catch (_: Exception) { /* A failed restore must leave the new playback queue alone. */ }
        }
    }

    private suspend fun report(nas: NasConnection) {
        checkPending()
        val atRevision = revision
        val h = JSONObject().put("epoch",epoch).put("sequence",++sequence)
            .put("playback",snapshot(sentRevision != atRevision))
        receipt?.let { h.put("result", it) }
        try {
            val update = withContext(Dispatchers.IO) { nas.sync(h) }
            if (NasPlayback.connection !== nas) return
            failures = 0
            awaitingLoadReceipt?.let { load ->
                if (!update.optBoolean("result_accepted", true)) pauseLoad(load)
                awaitingLoadReceipt = null
            }
            receipt = null
            sentRevision = if (update.optBoolean("need_queue")) "" else atRevision
            val cmd = update.optJSONObject("command")
            if (cmd != null && cmd.optString("id") != handled) perform(nas, cmd)
            else if (pending != null && cmd == null) cancelPending()
            recoverPlayback()
        } catch (e: CancellationException) { throw e }
        catch (e: Exception) {
            if (e is TvHttpError && e.status == 401) {
                NasPlayback.revoked.value = true
                NasPlayback.connection = null
                player.stop(); player.clearMediaItems()
            } else if (++failures >= 3 && reconnect?.isActive != true) {
                reconnect = scope.launch { rediscover(nas) }
            }
        }
    }

    private fun recoverPlayback() {
        if (player.isPlaying) retries = 0
        val error = player.playerError ?: return
        if (pending != null || !player.playWhenReady || retries >= 2 || SystemClock.elapsedRealtime() < retryAt) return
        if (error.errorCode == PlaybackException.ERROR_CODE_IO_NETWORK_CONNECTION_FAILED ||
            error.errorCode == PlaybackException.ERROR_CODE_IO_NETWORK_CONNECTION_TIMEOUT ||
            error.errorCode == PlaybackException.ERROR_CODE_IO_UNSPECIFIED) {
            retries++
            retryAt = SystemClock.elapsedRealtime() + 2_000L * retries
            player.prepare()
        }
    }

    private suspend fun rediscover(nas: NasConnection) {
        try {
            val target = withTimeoutOrNull(8_000) {
                discover(context).first { scan -> scan.servers.any { it.pin == nas.link.server.pin && it.address != nas.link.server.address } }
                    .servers.first { it.pin == nas.link.server.pin && it.address != nas.link.server.address }
            } ?: return
            val server = withContext(Dispatchers.IO) { TvClient.probe(target.address, nas.link.server.pin) }
            if (NasPlayback.connection !== nas) return
            val renewed = nas.link.copy(server = server)
            withContext(Dispatchers.IO) { TvCredentials(context).save(renewed) }
            val current = NasConnection(renewed)
            NasPlayback.connection = current
            NasPlayback.relocated.value = current
            if (player.playerError != null) player.prepare()
        } catch (e: CancellationException) { throw e }
        catch (_: Exception) { /* Try again after a failed report. */ }
    }

    private fun reply(id: String, success: Boolean, message: String = "") {
        receipt = JSONObject().put("id",id).put("state", if (success) "done" else "failed").put("message",message)
        pending = null
    }

    private fun cancelPending() {
        val waiting = pending ?: return
        pauseLoad(waiting)
        pending = null
    }

    private fun pauseLoad(waiting: Waiting) {
        if (waiting.action == "load" && player.currentMediaItem?.mediaId == waiting.entry) player.pause()
    }

    private fun checkPending() {
        val active = pending ?: return
        val phase = player.playbackState
        if (player.playerError != null) { cancelPending(); reply(active.id,false,"电视取流失败"); return }
        if (player.currentMediaItem?.mediaId != active.entry) { reply(active.id,false,"电视已切到另一首歌"); return }
        val timedOut = SystemClock.elapsedRealtime() >= active.until
        val done = when (active.action) {
            "seek" -> phase == Player.STATE_READY && kotlin.math.abs(player.currentPosition - active.position) <= 2100
            "load" -> {
                val position = player.currentPosition
                val first = active.firstPosition
                if (!player.isPlaying) active.firstPosition = null
                else if (first == null && kotlin.math.abs(position - active.position) <= 2500) active.firstPosition = position
                player.isPlaying && first != null && position > first
            }
            else -> player.isPlaying
        }
        if (timedOut) { cancelPending(); reply(active.id,false,"电视没有确认播放或定位") }
        else if (done) {
            if (active.action == "load") awaitingLoadReceipt = active
            reply(active.id,true)
        }
    }

    private suspend fun perform(nas: NasConnection, cmd: JSONObject) {
        val id = cmd.getString("id")
        val action = cmd.getString("action")
        handled = id
        cancelPending()
        val source = player.currentMediaItem?.mediaId.orEmpty()
        val expected = cmd.optString("entry")
        val requestedRevision = cmd.optString("revision")
        if ((expected.isNotEmpty() && expected != source) || (requestedRevision.isNotEmpty() && requestedRevision != revision)) {
            reply(id,false,"电视播放内容已变化"); return
        }
        val pos = cmd.optLong("position_ms")
        val index = cmd.optInt("index")
        try {
            when (action) {
                "load", "insert" -> {
                    val before = revision
                    val tracks = withContext(Dispatchers.IO) {
                        val found = nas.resolve(cmd.getJSONArray("items"))
                        if (!nas.pending(id)) return@withContext emptyList()
                        found
                    }
                    if (tracks.isEmpty()) return
                    if (NasPlayback.connection !== nas || revision != before || player.currentMediaItem?.mediaId.orEmpty() != source) {
                        reply(id,false,"电视播放内容已变化"); return
                    }
                    val media = tracks.map { it.mediaItem(true) }
                    if (action == "insert" && player.currentMediaItem?.identity != null) {
                        val at = if (cmd.optString("queue_mode") == "next") player.currentMediaItemIndex+1 else player.mediaItemCount
                        if (player.mediaItemCount >= MAX_QUEUE) { reply(id,false,"播放队列已满"); return }
                        player.addMediaItem(at,media.single()); reply(id,true); return
                    }
                    val start = if (action == "insert") 0 else index
                    player.setMediaItems(media,start,pos)
                    player.setMode(cmd.optString("mode").ifBlank { "loop" })
                    player.prepare(); player.play()
                    NasPlayback.remoteSelection.value = media[start].mediaId
                    pending = Waiting(id,media[start].mediaId,"load",SystemClock.elapsedRealtime()+17_000,pos)
                }
                "play" -> {
                    if (player.mediaItemCount == 0) { reply(id,false,"电视没有待播曲目"); return }
                    if (player.playbackState == Player.STATE_IDLE) player.prepare()
                    if (player.playbackState == Player.STATE_ENDED) player.seekToDefaultPosition(0)
                    player.play()
                    pending = Waiting(id,player.currentMediaItem?.mediaId.orEmpty(), action,SystemClock.elapsedRealtime()+17_000,0)
                }
                "pause" -> { player.pause(); reply(id,true) }
                "stop" -> { player.pause(); player.seekToDefaultPosition(); reply(id,true) }
                "next" -> { if (!player.hasNextMediaItem()) { reply(id,false,"没有下一首"); return }; player.seekToNextMediaItem(); reply(id,true) }
                "previous" -> { player.seekToPrevious(); reply(id,true) }
                "jump" -> {
                    if (index !in 0 until player.mediaItemCount) { reply(id,false,"曲目不在队列里"); return }
                    player.seekToDefaultPosition(index); player.play(); reply(id,true)
                }
                "remove" -> {
                    if (index !in 0 until player.mediaItemCount || player.currentMediaItemIndex == index) {
                        reply(id,false,"不能移除当前曲目"); return
                    }
                    player.removeMediaItem(index); reply(id,true)
                }
                "mode" -> { player.setMode(cmd.optString("mode")); reply(id,true) }
                "volume" -> {
                    player.volume = cmd.optInt("volume").coerceIn(0,100) / 100f
                    reply(id,true)
                }
                "seek" -> {
                    if (!player.isCurrentMediaItemSeekable) { reply(id,false,"这首歌不能定位"); return }
                    val target = if (player.duration > 0) pos.coerceAtMost((player.duration-50).coerceAtLeast(0)) else pos
                    player.seekTo(target)
                    pending = Waiting(id,source,action,SystemClock.elapsedRealtime()+12_000,target)
                }
            }
        } catch (e: CancellationException) { throw e }
        catch (_: Exception) { reply(id,false,"电视没有完成这次操作") }
    }
}
