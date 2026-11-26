package com.trackferry.tv

import android.content.Context
import android.net.nsd.NsdManager
import android.net.nsd.NsdServiceInfo
import android.os.Handler
import android.os.Looper
import okhttp3.HttpUrl
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.callbackFlow

/** A server found on the network: [name] is what to show (the NAS's own name), [service] keeps it unique. */
internal data class Discovered(val service: String, val address: HttpUrl, val pin: String?, val name: String = "", val version: String = "") {
    val title: String get() = name.ifBlank { "TrackFerry" }
}
internal data class Scan(val servers: List<Discovered> = emptyList(), val failed: Boolean = false)

/** Legacy NSD resolves one service at a time. Serialize callbacks and keep only services still present. */
@Suppress("DEPRECATION")
internal fun discover(context: Context) = callbackFlow {
    val nsd = context.getSystemService(NsdManager::class.java)
    val main = Handler(Looper.getMainLooper())
    val found = linkedMapOf<String, NsdServiceInfo>()
    val resolved = linkedMapOf<String, Discovered>()
    val pending = ArrayDeque<String>()
    var busy = false
    var closed = false
    fun publish() = trySend(Scan(resolved.values.sortedWith(compareBy({ it.title }, { it.service }))))
    fun next() {
        if (closed || busy) return
        val name = pending.removeFirstOrNull() ?: return
        val info = found[name]
        if (info == null) { next(); return }
        busy = true
        val done = object : NsdManager.ResolveListener {
            override fun onResolveFailed(info: NsdServiceInfo, code: Int) { main.post { busy = false; next() } }
            override fun onServiceResolved(info: NsdServiceInfo) {
                main.post {
                    if (!closed && found[name] != null) {
                        val address = runCatching { HttpUrl.Builder().scheme("https").host(info.host.hostAddress!!.substringBefore('%')).port(info.port).build() }.getOrNull()
                        val attrs = info.attributes
                        if (address != null && attrs["protocol"]?.decodeToString() == "1") {
                            resolved[name] = Discovered(name, address, attrs["id"]?.decodeToString(),
                                attrs["name"]?.decodeToString()?.trim().orEmpty().take(40), attrs["version"]?.decodeToString()?.trim().orEmpty().take(20))
                            publish()
                        }
                    }
                    busy = false
                    next()
                }
            }
        }
        try { nsd.resolveService(info, done) } catch (_: IllegalArgumentException) { busy = false; next() }
    }
    val listener = object : NsdManager.DiscoveryListener {
        override fun onDiscoveryStarted(type: String) = Unit
        override fun onDiscoveryStopped(type: String) = Unit
        override fun onStartDiscoveryFailed(type: String, code: Int) { trySend(Scan(failed = true)) }
        override fun onStopDiscoveryFailed(type: String, code: Int) = Unit
        override fun onServiceFound(info: NsdServiceInfo) {
            main.post {
                if (closed || info.serviceName in found || found.size >= 32) return@post
                found[info.serviceName] = info
                pending.addLast(info.serviceName)
                next()
            }
        }
        override fun onServiceLost(info: NsdServiceInfo) {
            main.post { found.remove(info.serviceName); resolved.remove(info.serviceName); publish() }
        }
    }
    try { nsd.discoverServices("_trackferry._tcp.", NsdManager.PROTOCOL_DNS_SD, listener) }
    catch (_: RuntimeException) { trySend(Scan(failed = true)) }
    awaitClose { main.post { closed = true; runCatching { nsd.stopServiceDiscovery(listener) } } }
}
