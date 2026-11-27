package com.trackferry.tv

import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.util.LruCache
import kotlinx.coroutines.flow.MutableStateFlow
import okhttp3.Call
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONArray
import org.json.JSONObject
import java.io.IOException
import java.math.BigInteger
import java.security.MessageDigest
import java.security.SecureRandom
import java.security.cert.CertificateException
import java.security.cert.X509Certificate
import java.util.Locale
import java.util.concurrent.TimeUnit
import javax.net.ssl.SSLContext
import javax.net.ssl.X509TrustManager

private const val API = "api/tv/v1"
internal const val MEDIA_ORIGIN = "https://trackferry.invalid/$API/audio"
internal data class Server(val address: HttpUrl, val name: String, val pin: String)
internal data class TvLink(val server: Server, val token: String)
internal data class PairAttempt(val server: Server, val id: String, val seed: String, val token: String) {
    val code get() = pairCode(server.pin, id, seed, sha256(token))
}
internal data class NasTrack(val id: Long, val title: String, val artist: String, val album: String, val albumId: String, val duration: Long,
                             val cover: String = "", val online: String = "", val source: String = "", val restricted: Boolean = false, val note: String = "")
internal data class SearchResults(val tracks: List<NasTrack>, val unavailable: List<String> = emptyList())
internal data class Collection(val title: String, val source: String, val id: String = "", val name: String = "", val artist: String = "", val cover: String = "", val count: Int = 0)
internal data class Shelf(val picks: List<NasTrack>, val collections: List<Collection>)
/** What a song really plays as: "FLAC 16bit 44.1kHz", "MP3 128k". */
internal data class TvTech(val label: String, val tier: String, val known: Boolean) {
    val shown get() = when (tier) { "hires" -> "高解析 $label"; "lossless" -> "无损 $label"; else -> label }
}
internal data class TvVersion(val track: NasTrack, val tech: TvTech, val error: String)
internal data class TrackLyrics(val lines: List<LyricLine>, val offset: Long)
internal class TvHttpError(val status: Int, message: String) : IOException(message)
private const val MAX_APP = 80L shl 20

/** The app of the server's version, as the server has it ready (see internal/update/tv.go). */
internal data class TvApp(val version: String, val abi: String, val phase: String, val done: Long, val total: Long, val size: Long, val sha256: String, val error: String) {
    companion object {
        fun of(o: JSONObject) = TvApp(o.optString("version"), o.optString("abi"), o.optString("phase"), o.optLong("done"), o.optLong("total"),
            o.optLong("size"), o.optString("sha256"), o.optString("error"))
    }
}

private val random = SecureRandom()
private fun ByteArray.hex() = joinToString("") { "%02x".format(Locale.ROOT, it.toInt() and 255) }
private fun sha256(bytes: ByteArray) = MessageDigest.getInstance("SHA-256").digest(bytes)
private fun sha256(s: String) = sha256(s.toByteArray(Charsets.UTF_8)).hex()
internal fun pairCode(pin: String, id: String, seed: String, tokenHash: String): String {
    val bytes = sha256("TrackFerry TV v1\n$pin\n$id\n$seed\n$tokenHash".toByteArray())
    return BigInteger(1, bytes.copyOf(8)).mod(BigInteger.valueOf(1_000_000)).toString().padStart(6, '0')
}

// Only an unauthenticated info probe can learn a key. All subsequent requests use an exact SPKI pin.
private class ServerTrust(private val expected: String?) : X509TrustManager {
    @Volatile var observed: String? = null
        private set

    override fun getAcceptedIssuers(): Array<X509Certificate> = emptyArray()
    override fun checkClientTrusted(chain: Array<out X509Certificate>?, authType: String?) { throw CertificateException("Unexpected client certificate") }
    override fun checkServerTrusted(chain: Array<out X509Certificate>?, authType: String?) {
        val leaf = chain?.firstOrNull() ?: throw CertificateException("Server key missing")
        leaf.checkValidity()
        val actual = sha256(leaf.publicKey.encoded).hex()
        if (expected != null && expected != actual) throw CertificateException("TrackFerry identity changed")
        observed = actual
    }
}

private fun client(trust: ServerTrust): OkHttpClient {
    val tls = SSLContext.getInstance("TLS")
    tls.init(null, arrayOf(trust), random)
    return OkHttpClient.Builder().sslSocketFactory(tls.socketFactory, trust)
        .hostnameVerifier { _, _ -> trust.observed != null }
        .followRedirects(false).followSslRedirects(false)
        .connectTimeout(4, TimeUnit.SECONDS).readTimeout(12, TimeUnit.SECONDS)
        .build()
}

internal fun serverAddress(raw: String): HttpUrl? {
    val entered = raw.trim()
    if (entered.isEmpty()) return null
    val explicit = "://" in entered
    val url = (if (explicit) entered else "https://$entered").toHttpUrlOrNull() ?: return null
    if (!url.isHttps || url.username.isNotEmpty() || url.password.isNotEmpty() || url.encodedPath != "/" || url.query != null || url.fragment != null) return null
    return if (!explicit && url.port == 443) url.newBuilder().port(18562).build() else url
}

internal object TvClient {
    fun probe(address: HttpUrl, expected: String? = null): Server {
        val trust = ServerTrust(expected)
        val probe = client(trust)
        try {
            val info = get(probe, address, "info", null)
            val pin = trust.observed ?: throw IOException("读不到服务器身份")
            if (info.optString("id") != pin || info.optInt("protocol") != 1) throw IOException("服务身份或版本不匹配")
            return Server(address, info.getString("name"), pin)
        } finally { probe.connectionPool.evictAll() }
    }

    fun begin(server: Server, name: String): PairAttempt {
        val seed = ByteArray(32).also(random::nextBytes).hex()
        val token = ByteArray(32).also(random::nextBytes).hex()
        val http = client(ServerTrust(server.pin))
        try {
            val body = JSONObject().put("name", name).put("commitment", sha256(seed)).put("token_hash", sha256(token))
            val id = post(http, server.address, "pair", body).getString("id")
            if (!id.matches(Regex("[0-9a-f]{32}"))) throw IOException("配对请求无效")
            val attempt = PairAttempt(server, id, seed, token)
            status(http, attempt) // makes this code visible to the authenticated approval endpoint
            return attempt
        } finally { http.connectionPool.evictAll() }
    }

    fun awaitApproval(attempt: PairAttempt): Boolean {
        val http = client(ServerTrust(attempt.server.pin))
        try { return status(http, attempt) == "approved" }
        finally { http.connectionPool.evictAll() }
    }

    fun cancel(attempt: PairAttempt) {
        val http = client(ServerTrust(attempt.server.pin))
        try { post(http, attempt.server.address, "pair/cancel", proof(attempt)) }
        finally { http.connectionPool.evictAll() }
    }

    private fun proof(attempt: PairAttempt): JSONObject = JSONObject().put("id", attempt.id).put("seed", attempt.seed)

    private fun status(http: OkHttpClient, attempt: PairAttempt): String = post(http, attempt.server.address, "pair/status",
        proof(attempt)).getString("status")

    private fun post(http: OkHttpClient, address: HttpUrl, path: String, request: JSONObject): JSONObject =
        json(http, Request.Builder().url(address.newBuilder().addPathSegments("$API/$path").build())
            .post(request.toString().toRequestBody("application/json".toMediaType())).build())

    internal fun get(http: OkHttpClient, address: HttpUrl, path: String, token: String?, query: List<Pair<String, String>> = emptyList()): JSONObject {
        val url = address.newBuilder().addPathSegments("$API/$path")
        for ((k, v) in query) url.addQueryParameter(k, v)
        val req = Request.Builder().url(url.build())
        if (token != null) req.header("Authorization", "Bearer $token")
        return json(http, req.build())
    }

    internal fun json(http: OkHttpClient, request: Request): JSONObject = http.newCall(request).execute().use { response ->
        val body = response.body ?: throw IOException("服务器没有返回数据")
        if (!response.isSuccessful) {
            val detail = runCatching { JSONObject(response.peekBody(8192).string()).optString("message") }.getOrNull()
            throw TvHttpError(response.code, detail?.takeIf { it.isNotBlank() } ?: "服务器暂不可用（${response.code}）")
        }
        val content = response.peekBody(32L * 1024 * 1024 + 1)
        if (content.contentLength() > 32L * 1024 * 1024) throw IOException("曲库响应过大")
        JSONObject(content.string())
    }
}

internal class NasConnection(val link: TvLink) {
    private val art = object : LruCache<String, Bitmap>(18 * 1024 * 1024) {
        override fun sizeOf(key: String, value: Bitmap) = value.allocationByteCount
    }
    private val http = client(ServerTrust(link.server.pin))
    private fun get(path: String, vararg query: Pair<String, String>) = TvClient.get(http, link.server.address, path, link.token, query.toList())
    fun shelf(batch: Long): Shelf {
        val result = get("shelf", "batch" to batch.toString())
        val lists = mutableListOf(Collection("我喜欢的音乐", "favorites", count = result.optInt("favorite_count")))
        result.optJSONArray("albums").objects().forEach { lists += Collection(it.getString("name"), "album", it.getString("id"), artist = it.optString("artist"), cover = it.optString("cover"), count = it.optInt("tracks")) }
        result.optJSONArray("artists").objects().forEach { lists += Collection(it.getString("name"), "artist", name = it.getString("name"), cover = it.optString("cover"), count = it.optInt("tracks")) }
        val playlists = try { playlists() }
        catch (e: TvHttpError) { if (e.status == 502) emptyList() else throw e }
        lists += playlists
        return Shelf(result.optJSONArray("picks").tracks(), lists)
    }
    fun search(q: String, online: Boolean = false): SearchResults {
        if (!online) return SearchResults(get("search", "q" to q).optJSONArray("tracks").tracks())
        val result = get("online", "q" to q)
        val tracks = result.optJSONArray("songs").objects().map {
            NasTrack(0, it.getString("title"), it.optString("artist"), it.optString("album"), "", it.optLong("duration_ms"),
                cover = it.optString("cover"), online = it.getString("key"), source = it.optString("source"), restricted = it.optBoolean("restricted"), note = it.optString("note"))
        }
        val unavailable = result.optJSONArray("sources").objects()
            .filter { it.optString("state") in setOf("error", "timeout", "open") }
            .map { it.getString("source") }.distinct()
        return SearchResults(tracks, unavailable)
    }
    fun playlists() = get("playlists").optJSONArray("playlists").objects().map {
        Collection(it.getString("name"), "playlist", it.getString("guid"), cover = it.optString("coverId").takeUnless { value -> value == "null" }.orEmpty(), count = it.optInt("trackCount"))
    }
    fun favorite(id: Long) = get("tracks/$id/favorite").getBoolean("favorite")
    fun setFavorite(id: Long, on: Boolean) = write("PUT", "tracks/$id/favorite", JSONObject().put("on", on)).getBoolean("favorite")
    fun addToPlaylist(guid: String, id: Long): Boolean {
        val path = link.server.address.newBuilder().addPathSegments("$API/playlists").addPathSegment(guid).addPathSegment("tracks").build()
        return write("POST", path, JSONObject().put("track_id", id)).getBoolean("changed")
    }
    private fun write(method: String, path: String, data: JSONObject) =
        write(method, link.server.address.newBuilder().addPathSegments("$API/$path").build(), data)
    private fun write(method: String, url: HttpUrl, data: JSONObject): JSONObject = TvClient.json(http, Request.Builder()
        .url(url).header("Authorization", "Bearer ${link.token}")
        .method(method, data.toString().toRequestBody("application/json".toMediaType())).build())
    private fun post(path: String, data: JSONObject): JSONObject = write("POST", path, data)
    fun sync(data: JSONObject) = post("sync",data)
    fun resolve(refs: JSONArray) = post("resolve",JSONObject().put("items",refs)).optJSONArray("tracks").tracks()
    fun pending(id: String) = get("pending", "command" to id).optBoolean("pending")
    fun queue(item: Collection) = get("queue", "source" to item.source, "id" to item.id, "name" to item.name).optJSONArray("tracks").tracks()
    /** Songs to go on with after one put on alone, as the browser does. */
    fun drift() = get("queue", "source" to "random").optJSONArray("tracks").tracks()
    /** Keeps this song's lyric offset on the server, for every end alike. */
    fun setLyricsOffset(id: Long, ms: Long) = write("PUT", "lyrics/$id/offset", JSONObject().put("offset_ms", ms)).optLong("offset_ms")
    fun appStatus(abi: String) = TvApp.of(get("app", "abi" to abi))
    fun prepareApp(abi: String) = TvApp.of(write("POST", link.server.address.newBuilder().addPathSegments("$API/app")
        .addQueryParameter("abi", abi).build(), JSONObject()))
    /**
     * Fetches the app the server checked and checks it once more here: the sum must be the one the
     * server read from the signed release, or the file is thrown away.
     */
    fun downloadApp(abi: String, into: java.io.File, expected: String, progress: (Long, Long) -> Unit) {
        val url = link.server.address.newBuilder().addPathSegments("$API/app/apk").addQueryParameter("abi", abi).build()
        val slow = http.newBuilder().readTimeout(60, TimeUnit.SECONDS).build()
        slow.newCall(Request.Builder().url(url).header("Authorization", "Bearer ${link.token}").build()).execute().use { response ->
            if (!response.isSuccessful) throw TvHttpError(response.code, runCatching { JSONObject(response.peekBody(8192).string()).optString("message") }
                .getOrNull()?.takeIf { it.isNotBlank() } ?: "取不到新版应用（${response.code}）")
            val sum = response.header("X-TrackFerry-SHA256").orEmpty()
            if (sum != expected || !sum.matches(Regex("[0-9a-f]{64}"))) throw IOException("新版应用的校验值不对")
            val body = response.body ?: throw IOException("服务器没有返回数据")
            val total = body.contentLength()
            if (total > MAX_APP) throw IOException("新版应用过大")
            val digest = MessageDigest.getInstance("SHA-256")
            var done = 0L
            into.outputStream().use { out ->
                body.byteStream().use { input ->
                    val buffer = ByteArray(64 * 1024)
                    while (true) {
                        val n = input.read(buffer)
                        if (n < 0) break
                        done += n
                        if (done > MAX_APP) throw IOException("新版应用过大")
                        digest.update(buffer, 0, n)
                        out.write(buffer, 0, n)
                        progress(done, total)
                    }
                }
            }
            if (digest.digest().hex() != expected) { into.delete(); throw IOException("新版应用下载不完整，请重试") }
        }
    }
    fun tech(ref: String): TvTech = (if (ref.toLongOrNull() != null) get("tech", "track" to ref) else get("tech", "online" to ref)).tech()
    fun versions(title: String, artist: String, fresh: Boolean = false) =
        get("versions", *listOfNotNull("title" to title, "artist" to artist, if (fresh) "fresh" to "1" else null).toTypedArray()).optJSONArray("versions").objects().map {
            TvVersion(NasTrack(it.optLong("track_id"), it.optString("title"), it.optString("artist"), it.optString("album"), "", it.optLong("duration_ms"),
                it.optString("cover"), it.optString("online"), it.optString("source")), it.optJSONObject("tech")?.tech() ?: TvTech("", "", false), it.optString("error"))
        }
    fun lyrics(id: Long): TrackLyrics {
        val response = get("lyrics/$id")
        return TrackLyrics(readLyrics(response.optString("lyrics")), response.optLong("offset_ms"))
    }
    /** Lyrics a platform has for an online song; its offset is kept on this screen only. */
    fun onlineLyrics(key: String) = TrackLyrics(readLyrics(get("online-lyrics", "key" to key).optString("lyrics")), 0)
    fun artwork(id: String, large: Boolean): Bitmap? {
        // A platform's cover is named by its address, so its id runs long.
        if (!id.matches(Regex("[A-Za-z0-9_-]{1,700}"))) return null
        val size = if (large) 600 else 160
        val key = "$id/$size"
        art.get(key)?.let { return it }
        val url = link.server.address.newBuilder().addPathSegments("$API/cover").addPathSegment(id)
            .addQueryParameter("size", size.toString()).build()
        val req = Request.Builder().url(url).header("Authorization", "Bearer ${link.token}").build()
        return http.newCall(req).execute().use { response ->
            if (!response.isSuccessful) return@use null
            val cap = 2L * 1024 * 1024
            val body = response.peekBody(cap + 1)
            if (body.contentLength() > cap) return@use null
            val data = body.bytes()
            val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
            BitmapFactory.decodeByteArray(data, 0, data.size, bounds)
            val side = maxOf(bounds.outWidth, bounds.outHeight)
            if (side !in 1..8192) return@use null
            val opts = BitmapFactory.Options().apply { inSampleSize = Integer.highestOneBit((side / size).coerceAtLeast(1)) }
            BitmapFactory.decodeByteArray(data, 0, data.size, opts)?.also { art.put(key, it) }
        }
    }

    fun audio(request: Request): Call {
        val url = link.server.address.newBuilder().addPathSegments("$API/audio")
        for (key in listOf("track","online","title","artist")) request.url.queryParameter(key)?.let { url.addQueryParameter(key,it) }
        return http.newCall(request.newBuilder().url(url.build()).header("Authorization", "Bearer ${link.token}").build())
    }
}

private fun JSONObject.tech() = TvTech(optString("label"), optString("tier"), optBoolean("known"))
private fun JSONArray?.objects(): List<JSONObject> = if (this == null) emptyList() else List(length(), ::getJSONObject)
private fun JSONArray?.tracks() = objects().map {
    NasTrack(it.getLong("id"), it.getString("title"), it.optString("artist"), it.optString("album"), it.optString("album_id"), it.optLong("duration_ms"), it.optString("cover"), it.optString("online"))
}

internal object NasPlayback {
    @Volatile var connection: NasConnection? = null
    @Volatile var visible = false
    val revoked = MutableStateFlow(false)
    val remoteSelection = MutableStateFlow<String?>(null)
    val relocated = MutableStateFlow<NasConnection?>(null)
    val calls = Call.Factory { request ->
        if (request.url.host != "trackferry.invalid" || request.url.encodedPath != "/$API/audio") throw IOException("不支持的音频地址")
        (connection ?: throw IOException("电视尚未连接曲库")).audio(request)
    }
}
