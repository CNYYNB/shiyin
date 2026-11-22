plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "com.louis.myplayer"
    compileSdk = 34

    defaultConfig {
        applicationId = "com.louis.myplayer"
        minSdk = 24
        targetSdk = 34
        versionCode = 17
        versionName = "2.3.2"
        // Where version.json + the APK are hosted. Override with -PupdateBaseUrl=https://… if it moves.
        val updateBaseUrl = (project.findProperty("updateBaseUrl") as String?) ?: "https://shiyin.example.com"
        buildConfigField("String", "UPDATE_BASE_URL", "\"$updateBaseUrl\"")
    }

    buildFeatures { buildConfig = true }

    signingConfigs {
        create("mykey") {
            storeFile = rootProject.file("keystore/myplayer-debug.jks")
            storePassword = "android"
            keyAlias = "myplayer"
            keyPassword = "android"
        }
    }

    buildTypes {
        getByName("release") {
            isMinifyEnabled = false
            signingConfig = signingConfigs.getByName("mykey")
        }
        getByName("debug") {
            signingConfig = signingConfigs.getByName("mykey")
        }
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions { jvmTarget = "17" }
    testOptions { unitTests.isReturnDefaultValues = true }
    lint { checkReleaseBuilds = false; abortOnError = false }
    packaging { resources { excludes += setOf("META-INF/*.kotlin_module", "META-INF/AL2.0", "META-INF/LGPL2.1") } }
}

dependencies {
    implementation("androidx.core:core-ktx:1.13.1")
    implementation("androidx.activity:activity-ktx:1.9.3")
    implementation("androidx.webkit:webkit:1.11.0")
    implementation("androidx.media3:media3-exoplayer:1.4.1")
    implementation("androidx.media3:media3-session:1.4.1")
    implementation("androidx.documentfile:documentfile:1.0.1")
    testImplementation("junit:junit:4.13.2")
    testImplementation("org.json:json:20231013") // real org.json for JVM unit tests (android.jar only has stubs)
}
