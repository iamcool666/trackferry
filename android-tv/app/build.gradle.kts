import com.android.build.api.variant.FilterConfiguration
import java.util.Properties

plugins {
    id("com.android.application") version "9.0.1"
    id("org.jetbrains.kotlin.plugin.compose") version "2.2.10"
}

// The television app carries the server's version (VERSION at the repository root), so 我的 →
// 应用更新 can tell whether it is behind the server.
val release: String = rootProject.file("../VERSION").readText().trim()
val parts = release.split('.').map { it.toInt() }
require(parts.size == 3 && parts.all { it in 0..99 }) { "VERSION must read like 1.6.0" }
val base = parts[0] * 10_000 + parts[1] * 100 + parts[2]

// The release key stays in .private (never in Git). Without it a release build stays unsigned
// and scripts/publish.sh refuses to upload it.
val keyFile = rootProject.file("../.private/trackferry-tv.properties")
val key = Properties().apply { if (keyFile.isFile) keyFile.inputStream().use(::load) }

android {
    namespace = "com.trackferry.tv"
    compileSdk = 36

    defaultConfig {
        applicationId = "com.trackferry.tv"
        minSdk = 23
        targetSdk = 36
        versionCode = base * 10
        versionName = release
    }

    signingConfigs {
        if (keyFile.isFile) create("release") {
            storeFile = keyFile.parentFile.resolve(key.getProperty("storeFile"))
            storePassword = key.getProperty("storePassword")
            keyAlias = key.getProperty("keyAlias")
            keyPassword = key.getProperty("keyPassword")
        }
    }

    buildTypes {
        release {
            // Shrinking stays off: nothing here is tested against a shrunk build on a real set.
            isMinifyEnabled = false
            if (keyFile.isFile) signingConfig = signingConfigs.getByName("release")
        }
    }

    // One app for 32-bit and one for 64-bit televisions.
    splits {
        abi {
            isEnable = true
            reset()
            include("armeabi-v7a", "arm64-v8a")
            isUniversalApk = false
        }
    }

    buildFeatures {
        compose = true
    }
}

// The 64-bit app counts one higher, so a set that can run it moves up to it.
androidComponents {
    onVariants { variant ->
        variant.outputs.forEach { output ->
            val abi = output.filters.firstOrNull { it.filterType == FilterConfiguration.FilterType.ABI }?.identifier
            output.versionCode.set(base * 10 + when (abi) { "arm64-v8a" -> 2; "armeabi-v7a" -> 1; else -> 0 })
        }
    }
}

dependencies {
    implementation(platform("androidx.compose:compose-bom:2025.12.00"))
    implementation("androidx.activity:activity-compose:1.12.2")
    implementation("androidx.compose.foundation:foundation")
    implementation("androidx.tv:tv-material:1.1.0")

    implementation("androidx.media3:media3-exoplayer:1.9.4")
    implementation("androidx.media3:media3-session:1.9.4")
    implementation("androidx.media3:media3-datasource-okhttp:1.9.4")

    testImplementation("junit:junit:4.13.2")
}
