package com.louis.myplayer

import android.content.Context
import android.content.ContextWrapper
import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File
import java.nio.file.Files

/** USB fallback: 拾音数据.json + .拾音封面/ written into the 拾音 folder (format: shiyin-sync-protocol.md「USB 数据文件」). */
class ExporterTest {
    private class Ctx(val dir: File) : ContextWrapper(null) {
        override fun getFilesDir(): File = File(dir, "files").apply { mkdirs() }
        override fun getCacheDir(): File = File(dir, "cache").apply { mkdirs() }
        override fun getApplicationContext(): Context = this
    }

    @Test fun exportsPlaylistsNamesAndCovers() {
        val tmp = Files.createTempDirectory("shiyin").toFile()
        val ctx = Ctx(tmp)
        Library.init(ctx)
        val home = File(tmp, "Music/拾音"); Library.homeOverride = home
        val t1 = Track("m1", "content://1", "Sunny Day", "Jay Chou", "Ye Hui Mei", 1, 269000, "/sdcard/Music/拾音/Jay Chou - Sunny Day.flac", "/Music", 0, 3, 33300000, "", "Jay Chou - Sunny Day.flac")
        val t2 = Track("m2", "content://2", "Ten Years", "Eason", "", 2, 200000, "/sdcard/Music/拾音/Eason - Ten Years (2).mp3", "/Music", 0, 1, 4000000, "", "Eason - Ten Years (2).mp3")
        // Library.setTracks is private: tests set the list directly
        Library::class.java.getDeclaredMethod("setTracks", List::class.java).apply { isAccessible = true }.invoke(Library, listOf(t1, t2))
        Meta.set(t1.key, "晴天", "周杰伦", "叶惠美", "manual")
        Kv.set(ctx, "playlists", JSONArray()
            .put(JSONObject().put("id", "fav").put("name", "我喜欢").put("ids", JSONArray().put("m1").put("gone")))
            .put(JSONObject().put("id", "p1").put("name", "跑步").put("ids", JSONArray().put("m2").put("m1"))).toString())
        val cover = Covers.customFile(ctx, t2); cover.parentFile!!.mkdirs(); cover.writeBytes(ByteArray(500) { it.toByte() })

        Exporter.export(ctx)
        val f = File(home, Exporter.FILE_NAME)
        assertTrue(f.exists())
        val d = JSONObject(f.readText())
        assertEquals("shiyin", d.getString("app")); assertEquals("shiyin-data", d.getString("kind")); assertEquals(1, d.getInt("format"))
        val tr = d.getJSONObject("tracks")
        assertEquals("晴天", tr.getJSONObject("jay chou - sunny day|33300000").getString("title"))
        assertEquals("周杰伦", tr.getJSONObject("jay chou - sunny day|33300000").getString("artist"))
        val c2 = tr.getJSONObject("eason - ten years|4000000") // " (2)" stripped from the key
        assertFalse(c2.has("title"))
        val cf = File(home, Exporter.COVER_DIR + "/" + c2.getString("cover"))
        assertTrue(cf.exists()); assertEquals(500L, cf.length())
        val pls = d.getJSONArray("playlists")
        assertEquals(2, pls.length())
        assertTrue(pls.getJSONObject(0).getBoolean("fav"))
        assertEquals(listOf("jay chou - sunny day|33300000"), (0 until pls.getJSONObject(0).getJSONArray("keys").length()).map { pls.getJSONObject(0).getJSONArray("keys").getString(it) })
        assertEquals("跑步", pls.getJSONObject(1).getString("name"))
        assertEquals(2, pls.getJSONObject(1).getJSONArray("keys").length())

        // unchanged → file not rewritten (same "updated")
        val updated = d.getLong("updated")
        Thread.sleep(20); Exporter.export(ctx)
        assertEquals(updated, JSONObject(f.readText()).getLong("updated"))

        // cover removed → its copy is deleted too; turned off → no more writes
        Covers.clearCustom(ctx, listOf(t2)); Exporter.export(ctx)
        assertFalse(cf.exists())
        assertFalse(JSONObject(f.readText()).getJSONObject("tracks").has("eason - ten years|4000000"))
        Kv.set(ctx, "exportData", "false")
        Meta.set(t2.key, "十年", "陈奕迅", null, "manual"); Exporter.export(ctx)
        assertFalse(JSONObject(f.readText()).getJSONObject("tracks").has("eason - ten years|4000000"))
        Library.homeOverride = null
    }
}
