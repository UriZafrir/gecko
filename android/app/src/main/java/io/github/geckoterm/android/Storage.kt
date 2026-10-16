package io.github.geckoterm.android

import android.content.Context
import android.content.SharedPreferences
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

class PrefsKeyValue(private val prefs: SharedPreferences) : KeyValue {
    override fun get(key: String): String? = prefs.getString(key, null)
    override fun put(key: String, value: String?) {
        prefs.edit().apply { if (value == null) remove(key) else putString(key, value) }.apply()
    }
}

/** AES-GCM with a key generated inside the Android Keystore; it can't be read out. */
class KeystoreBox(private val alias: String = "gecko-tokens") : SecretBox {
    private fun key(): SecretKey {
        val ks = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        (ks.getKey(alias, null) as? SecretKey)?.let { return it }
        val gen = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore")
        gen.init(
            KeyGenParameterSpec.Builder(alias, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(256)
                .build(),
        )
        return gen.generateKey()
    }

    override fun seal(plain: String): String {
        val c = Cipher.getInstance("AES/GCM/NoPadding")
        c.init(Cipher.ENCRYPT_MODE, key())
        val ct = c.doFinal(plain.toByteArray(Charsets.UTF_8))
        return "v1:" + Base64.encodeToString(c.iv + ct, Base64.NO_WRAP)
    }

    override fun open(sealed: String): String? = try {
        val b = Base64.decode(sealed.removePrefix("v1:"), Base64.NO_WRAP)
        val c = Cipher.getInstance("AES/GCM/NoPadding")
        c.init(Cipher.DECRYPT_MODE, key(), GCMParameterSpec(128, b, 0, 12))
        String(c.doFinal(b, 12, b.size - 12), Charsets.UTF_8)
    } catch (e: Exception) {
        null
    }
}

/** The app's settings and saved machines. */
object Gecko {
    @Volatile private var store: MachineStore? = null

    fun machines(ctx: Context): MachineStore = store ?: synchronized(this) {
        store ?: MachineStore(PrefsKeyValue(ctx.applicationContext.getSharedPreferences("machines", Context.MODE_PRIVATE)), KeystoreBox()).also { store = it }
    }

    fun prefs(ctx: Context): SharedPreferences = ctx.applicationContext.getSharedPreferences("settings", Context.MODE_PRIVATE)

    const val KEEP_SCREEN_ON = "keepScreenOn"
    const val BACKGROUND = "stayConnected"

    /** The machine shown in the app right now (null when the app isn't in front). */
    @Volatile var foregroundMachine: String? = null
}
