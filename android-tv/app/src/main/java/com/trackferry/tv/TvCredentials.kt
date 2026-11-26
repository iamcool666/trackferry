package com.trackferry.tv

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import okhttp3.HttpUrl.Companion.toHttpUrl
import org.json.JSONObject
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

internal class TvCredentials(context: Context) {
    private val preferences = context.getSharedPreferences("tv_connection", Context.MODE_PRIVATE)
    private val alias = "trackferry.tv.connection"
    private fun key(): SecretKey {
        val store = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        (store.getKey(alias, null) as? SecretKey)?.let { return it }
        return KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore").apply {
            init(KeyGenParameterSpec.Builder(alias, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM).setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE).build())
        }.generateKey()
    }
    fun load(): TvLink? = runCatching {
        val encoded = preferences.getString("link", null) ?: return null
        val data = Base64.decode(encoded, Base64.NO_WRAP)
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.DECRYPT_MODE, key(), GCMParameterSpec(128, data.copyOfRange(0, 12)))
        val saved = JSONObject(cipher.doFinal(data.copyOfRange(12, data.size)).toString(Charsets.UTF_8))
        TvLink(Server(saved.getString("url").toHttpUrl(), saved.getString("name"), saved.getString("pin")), saved.getString("token"))
    }.getOrNull()
    fun save(link: TvLink) {
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.ENCRYPT_MODE, key())
        val payload = JSONObject().put("url", link.server.address.toString()).put("name", link.server.name)
            .put("pin", link.server.pin).put("token", link.token)
        val blob = cipher.iv + cipher.doFinal(payload.toString().toByteArray())
        check(preferences.edit().putString("link", Base64.encodeToString(blob, Base64.NO_WRAP)).commit()) { "无法保存电视配对" }
    }
    fun clear() { preferences.edit().remove("link").apply() }
}
