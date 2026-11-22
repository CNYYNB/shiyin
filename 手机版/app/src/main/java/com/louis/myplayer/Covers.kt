package com.louis.myplayer

import android.content.ContentUris
import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.media.MediaMetadataRetriever
import android.net.Uri
import android.os.Build
import android.util.Size
import java.io.File
import java.io.FileOutputStream

/** Extracts and caches cover art as small JPEGs in cacheDir/covers. */
object Covers {
    private const val MAX = 600
    private val locks = HashMap<String, Any>()

    private fun dir(ctx: Context) = File(ctx.cacheDir, "covers").apply { mkdirs() }

    private fun lockFor(id: String): Any = synchronized(locks) { locks.getOrPut(id) { Any() } }

    // ---- covers the user picked (photo / online), keyed by Track.key, kept in filesDir ----
    private fun customDir(ctx: Context) = File(ctx.filesDir, "covers_custom").apply { mkdirs() }
    private fun md5(s: String) = java.security.MessageDigest.getInstance("MD5").digest(s.toByteArray()).joinToString("") { "%02x".format(it) }
    fun customFile(ctx: Context, t: Track) = File(customDir(ctx), md5(t.key) + ".jpg")
    /** Changes whenever the custom cover changes (0 = none); the UI appends it to the cover URL to bust the WebView cache. */
    fun customVersion(ctx: Context, t: Track): Long = customFile(ctx, t).let { if (it.exists()) it.lastModified() else 0L }

    /** Saves [data] (any image) as the cover of every track in [list]. Returns false if it isn't a decodable image. */
    fun setCustom(ctx: Context, list: List<Track>, data: ByteArray): Boolean {
        val bmp = decodeScaled(data) ?: return false
        val bytes = java.io.ByteArrayOutputStream().also { bmp.compress(Bitmap.CompressFormat.JPEG, 90, it) }.toByteArray()
        bmp.recycle()
        for (t in list) {
            val f = customFile(ctx, t)
            val tmp = File(f.parentFile, f.name + ".tmp")
            tmp.writeBytes(bytes)
            if (!tmp.renameTo(f)) { f.writeBytes(bytes); tmp.delete() }
            dropCache(ctx, t)
        }
        Exporter.schedule(ctx)
        return true
    }

    fun clearCustom(ctx: Context, list: List<Track>) { for (t in list) { customFile(ctx, t).delete(); dropCache(ctx, t) }; Exporter.schedule(ctx) }

    private fun dropCache(ctx: Context, t: Track) { File(dir(ctx), t.id + ".jpg").delete(); File(dir(ctx), t.id + ".none").delete() }

    /** Returns a cached JPEG file for the track, or null if it has no art. Blocking. */
    fun file(ctx: Context, t: Track): File? {
        customFile(ctx, t).let { if (it.exists()) return it }
        val d = dir(ctx)
        val f = File(d, t.id + ".jpg")
        val none = File(d, t.id + ".none")
        if (f.exists()) return f
        if (none.exists()) return null
        synchronized(lockFor(t.id)) {
            if (f.exists()) return f
            if (none.exists()) return null
            val bmp = extract(ctx, t)
            if (bmp == null) { none.createNewFile(); return null }
            FileOutputStream(f).use { bmp.compress(Bitmap.CompressFormat.JPEG, 88, it) }
            bmp.recycle()
            return f
        }
    }

    fun bitmap(ctx: Context, t: Track): Bitmap? = file(ctx, t)?.let { BitmapFactory.decodeFile(it.path) }

    fun clear(ctx: Context) { dir(ctx).listFiles()?.forEach { it.delete() } }

    private fun extract(ctx: Context, t: Track): Bitmap? {
        val uri = Uri.parse(t.uri)
        // 1. embedded picture
        val mmr = MediaMetadataRetriever()
        try {
            mmr.setDataSource(ctx, uri)
            val pic = mmr.embeddedPicture
            if (pic != null) decodeScaled(pic)?.let { return it }
        } catch (_: Exception) {
        } finally { try { mmr.release() } catch (_: Exception) {} }
        // 2. MediaStore thumbnail / album art
        if (t.id.startsWith("m")) {
            if (Build.VERSION.SDK_INT >= 29) {
                try { return ctx.contentResolver.loadThumbnail(uri, Size(MAX, MAX), null) } catch (_: Exception) {}
            } else if (t.albumId > 0) {
                try {
                    val art = ContentUris.withAppendedId(Uri.parse("content://media/external/audio/albumart"), t.albumId)
                    ctx.contentResolver.openInputStream(art)?.use { s -> decodeScaled(s.readBytes())?.let { return it } }
                } catch (_: Exception) {}
            }
        }
        // 3. folder.jpg / cover.jpg next to the file
        if (t.path.isNotEmpty()) {
            val parent = File(t.path).parentFile
            for (n in listOf("cover.jpg", "folder.jpg", "cover.png", "folder.png", "Cover.jpg", "Folder.jpg")) {
                try {
                    val f = File(parent, n)
                    if (f.canRead()) decodeScaled(f.readBytes())?.let { return it }
                } catch (_: Exception) {}
            }
        }
        return null
    }

    private fun decodeScaled(data: ByteArray): Bitmap? {
        val o = BitmapFactory.Options().apply { inJustDecodeBounds = true }
        BitmapFactory.decodeByteArray(data, 0, data.size, o)
        if (o.outWidth <= 0) return null
        var sample = 1
        while (o.outWidth / (sample * 2) >= MAX && o.outHeight / (sample * 2) >= MAX) sample *= 2
        val bmp = BitmapFactory.decodeByteArray(data, 0, data.size, BitmapFactory.Options().apply { inSampleSize = sample }) ?: return null
        val big = maxOf(bmp.width, bmp.height)
        if (big <= MAX) return bmp
        val s = MAX.toFloat() / big
        val scaled = Bitmap.createScaledBitmap(bmp, (bmp.width * s).toInt().coerceAtLeast(1), (bmp.height * s).toInt().coerceAtLeast(1), true)
        if (scaled != bmp) bmp.recycle()
        return scaled
    }
}
