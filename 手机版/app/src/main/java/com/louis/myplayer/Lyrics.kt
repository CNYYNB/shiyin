package com.louis.myplayer

import android.content.Context
import android.net.Uri
import android.util.Log
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.io.InputStream
import java.net.HttpURLConnection
import java.net.URL
import java.net.URLEncoder
import java.nio.ByteBuffer
import java.nio.charset.Charset
import java.nio.charset.CodingErrorAction
import java.security.MessageDigest

/**
 * Lyrics lookup, in order:
 *  0. a lyric the user picked manually (cache, source=manual)
 *  1. same-name .lrc next to the audio file
 *  2. embedded lyrics (ID3 USLT/SYLT-less, FLAC/Vorbis LYRICS, MP4 ©lyr)
 *  3. cached online result
 *  4. online lookup - QQ音乐 → 酷狗 → 网易云 → LRCLIB, first synced (滚动) lyric wins, plain text only as a last resort.
 *     The result is cached, and also written as a same-name .lrc next to songs in 内部存储/Music/拾音.
 */
object Lyrics {
    private const val TAG = "Lyrics"
    private const val UA = "MyPlayer/1.0 (private Android music player; com.louis.myplayer)"
    private const val MISS_RETRY_MS = 3L * 24 * 3600 * 1000

    private fun dir(ctx: Context) = File(ctx.filesDir, "lyrics").apply { mkdirs() }

    fun key(t: Track): String {
        val md = MessageDigest.getInstance("MD5").digest("${t.title}|${t.artist}|${t.duration / 1000}".toByteArray())
        return md.joinToString("") { "%02x".format(it) }
    }

    private fun result(source: String, text: String) = JSONObject()
        .put("source", source).put("text", text).put("synced", isSynced(text))

    fun isSynced(text: String) = Regex("\\[\\d{1,3}:\\d{1,2}([.:]\\d{1,3})?]").containsMatchIn(text)

    /** Blocking. Returns JSON {source,text,synced} ; source = none when nothing found. */
    fun load(ctx: Context, t: Track, force: Boolean): JSONObject {
        val d = dir(ctx)
        val k = key(t)
        val manual = File(d, "$k.manual.json")
        if (manual.exists()) try { return JSONObject(manual.readText()) } catch (_: Exception) {}

        if (!force) {
            sidecar(ctx, t)?.let { return result("lrc", it) }
            try { embedded(ctx, t)?.let { if (it.isNotBlank()) return result("embedded", it) } } catch (e: Exception) { Log.w(TAG, "embedded", e) }
        }
        val cache = File(d, "$k.json")
        if (!force && cache.exists()) try {
            val c = JSONObject(cache.readText())
            // results from older versions that only had plain text get one more try with the new sources
            if (c.optBoolean("synced") || c.optInt("v") >= CACHE_V) return c
        } catch (_: Exception) {}
        val miss = File(d, "$k.miss")
        if (!force && miss.exists() && System.currentTimeMillis() - miss.lastModified() < MISS_RETRY_MS) {
            return JSONObject().put("source", "none").put("text", "").put("synced", false)
        }
        return try {
            val online = fetchOnline(t)
            if (online != null) {
                online.put("v", CACHE_V)
                cache.writeText(online.toString()); miss.delete()
                persistLrc(ctx, t, online.optString("text"))
                online
            } else {
                miss.writeText(System.currentTimeMillis().toString())
                JSONObject().put("source", "none").put("text", "").put("synced", false)
            }
        } catch (e: Exception) {
            Log.w(TAG, "online", e)
            JSONObject().put("source", "none").put("text", "").put("synced", false).put("error", "网络错误：" + (e.message ?: e.javaClass.simpleName))
        }
    }

    /** Lyrics already on the phone (manual, sidecar .lrc, embedded, cached online result). Never goes online. */
    fun saved(ctx: Context, t: Track): String? {
        val d = dir(ctx); val k = key(t)
        try { File(d, "$k.manual.json").takeIf { it.exists() }?.let { return JSONObject(it.readText()).optString("text").ifBlank { null } } } catch (_: Exception) {}
        sidecar(ctx, t)?.let { if (it.isNotBlank()) return it }
        try { embedded(ctx, t)?.let { if (it.isNotBlank()) return it } } catch (_: Exception) {}
        try { File(d, "$k.json").takeIf { it.exists() }?.let { return JSONObject(it.readText()).optString("text").ifBlank { null } } } catch (_: Exception) {}
        return null
    }

    fun saveManual(ctx: Context, t: Track, text: String, synced: Boolean) {
        val o = JSONObject().put("source", "manual").put("text", text).put("synced", synced || isSynced(text))
        File(dir(ctx), key(t) + ".manual.json").writeText(o.toString())
        persistLrc(ctx, t, text, overwriteOwn = true)
    }

    fun clearManual(ctx: Context, t: Track) {
        val k = key(t)
        File(dir(ctx), "$k.manual.json").delete(); File(dir(ctx), "$k.json").delete(); File(dir(ctx), "$k.miss").delete()
        // an .lrc we wrote ourselves is dropped too, so auto-matching really starts over
        val own = File(dir(ctx), "$k.wrote")
        if (own.exists()) { try { File(own.readText()).delete() } catch (_: Exception) {}; own.delete() }
    }

    private const val CACHE_V = 2

    /** Saves lyrics as <song>.lrc beside songs that live in 拾音's folder, so they're kept with the music. */
    private fun persistLrc(ctx: Context, t: Track, text: String, overwriteOwn: Boolean = false) {
        try {
            if (text.isBlank() || t.path.isEmpty()) return
            val home = Library.homeDir().absolutePath + "/"
            if (!t.path.startsWith(home)) return
            val lrc = File(t.path.substringBeforeLast('.') + ".lrc")
            val own = File(dir(ctx), key(t) + ".wrote")
            if (lrc.exists() && !(overwriteOwn && own.exists())) return
            if (!lrc.parentFile!!.canWrite()) return
            lrc.writeText(text, Charsets.UTF_8)
            own.writeText(lrc.absolutePath)
        } catch (e: Exception) { Log.w(TAG, "persistLrc", e) }
    }

    // ---------- sidecar .lrc ----------
    private fun sidecar(ctx: Context, t: Track): String? {
        try {
            if (t.lrcUri.isNotEmpty()) ctx.contentResolver.openInputStream(Uri.parse(t.lrcUri))?.use { return decodeText(it.readBytes()) }
        } catch (_: Exception) {}
        if (t.path.isNotEmpty()) {
            val base = t.path.substringBeforeLast('.')
            for (ext in listOf(".lrc", ".LRC", ".Lrc")) {
                try {
                    val f = File(base + ext)
                    if (f.exists() && f.canRead()) return decodeText(f.readBytes())
                } catch (_: Exception) {}
            }
        }
        return null
    }

    fun decodeText(b: ByteArray): String {
        if (b.size >= 3 && b[0] == 0xEF.toByte() && b[1] == 0xBB.toByte() && b[2] == 0xBF.toByte()) return String(b, 3, b.size - 3, Charsets.UTF_8)
        if (b.size >= 2 && b[0] == 0xFF.toByte() && b[1] == 0xFE.toByte()) return String(b, 2, b.size - 2, Charsets.UTF_16LE)
        if (b.size >= 2 && b[0] == 0xFE.toByte() && b[1] == 0xFF.toByte()) return String(b, 2, b.size - 2, Charsets.UTF_16BE)
        return strict(b, Charsets.UTF_8) ?: strict(b, Charset.forName("GB18030")) ?: String(b, Charsets.ISO_8859_1)
    }

    private fun strict(b: ByteArray, cs: Charset): String? = try {
        cs.newDecoder().onMalformedInput(CodingErrorAction.REPORT).onUnmappableCharacter(CodingErrorAction.REPORT)
            .decode(ByteBuffer.wrap(b)).toString()
    } catch (_: Exception) { null }

    // ---------- embedded ----------
    private fun embedded(ctx: Context, t: Track): String? {
        val uri = Uri.parse(t.uri)
        val head = ctx.contentResolver.openInputStream(uri)?.use { readUpTo(it, 16) } ?: return null
        if (head.size < 8) return null
        val magic = String(head, 0, 4, Charsets.ISO_8859_1)
        return when {
            magic.startsWith("ID3") -> ctx.contentResolver.openInputStream(uri)?.use { id3(it) }
            magic == "fLaC" -> ctx.contentResolver.openInputStream(uri)?.use { flac(it) }
            magic == "OggS" -> ctx.contentResolver.openInputStream(uri)?.use { vorbisScan(readUpTo(it, 4 shl 20)) }
            String(head, 4, 4, Charsets.ISO_8859_1) == "ftyp" ->
                if (t.size in 1..(150L shl 20)) ctx.contentResolver.openInputStream(uri)?.use { mp4Lyr(it.readBytes()) } else null
            else -> null
        }
    }

    private fun readUpTo(s: InputStream, n: Int): ByteArray {
        val buf = ByteArray(n); var off = 0
        while (off < n) { val r = s.read(buf, off, n - off); if (r < 0) break; off += r }
        return if (off == n) buf else buf.copyOf(off)
    }

    private fun id3(s: InputStream): String? {
        val h = readUpTo(s, 10)
        if (h.size < 10) return null
        val ver = h[3].toInt()
        val flags = h[5].toInt()
        val size = syncsafe(h, 6)
        if (size <= 0 || size > (64 shl 20)) return null
        var tag = readUpTo(s, size)
        if (ver < 4 && flags and 0x80 != 0) tag = unsync(tag)
        var p = 0
        if (flags and 0x40 != 0 && tag.size >= 4) { // extended header
            p = if (ver >= 4) syncsafe(tag, 0) else be32(tag, 0) + 4
        }
        var txxx: String? = null
        while (p + 10 <= tag.size) {
            val id = String(tag, p, 4, Charsets.ISO_8859_1)
            if (id[0] == '\u0000') break
            val fsz = if (ver >= 4) syncsafe(tag, p + 4) else be32(tag, p + 4)
            val fflags2 = tag[p + 9].toInt()
            val start = p + 10
            if (fsz <= 0 || start + fsz > tag.size) break
            var data = tag.copyOfRange(start, start + fsz)
            if (ver >= 4 && fflags2 and 0x02 != 0) data = unsync(data)
            if (ver >= 4 && fflags2 and 0x01 != 0 && data.size > 4) data = data.copyOfRange(4, data.size) // data length indicator
            if (id == "USLT" && data.size > 4) {
                val enc = data[0].toInt()
                var q = 4
                q = skipTerminated(data, q, enc)
                val text = decodeId3(data.copyOfRange(q.coerceAtMost(data.size), data.size), enc).trim('\u0000', ' ', '\n', '\r')
                if (text.isNotBlank()) return text
            } else if (id == "TXXX" && data.size > 2) {
                val enc = data[0].toInt()
                val q = skipTerminated(data, 1, enc)
                val desc = decodeId3(data.copyOfRange(1, q.coerceAtMost(data.size)), enc).trim('\u0000').uppercase()
                if (desc == "LYRICS" || desc == "UNSYNCEDLYRICS" || desc == "SYNCEDLYRICS")
                    txxx = decodeId3(data.copyOfRange(q.coerceAtMost(data.size), data.size), enc).trim('\u0000')
            }
            p = start + fsz
        }
        return txxx
    }

    private fun skipTerminated(d: ByteArray, from: Int, enc: Int): Int {
        var q = from
        if (enc == 1 || enc == 2) {
            while (q + 1 < d.size && !(d[q] == 0.toByte() && d[q + 1] == 0.toByte())) q += 2
            return q + 2
        }
        while (q < d.size && d[q] != 0.toByte()) q++
        return q + 1
    }

    private fun decodeId3(b: ByteArray, enc: Int): String = when (enc) {
        1 -> if (b.size >= 2 && b[0] == 0xFE.toByte() && b[1] == 0xFF.toByte()) String(b, 2, b.size - 2, Charsets.UTF_16BE)
             else if (b.size >= 2 && b[0] == 0xFF.toByte() && b[1] == 0xFE.toByte()) String(b, 2, b.size - 2, Charsets.UTF_16LE)
             else String(b, Charsets.UTF_16LE)
        2 -> String(b, Charsets.UTF_16BE)
        3 -> String(b, Charsets.UTF_8)
        else -> if (b.any { it < 0 }) (strict(b, Charsets.UTF_8) ?: strict(b, Charset.forName("GB18030")) ?: String(b, Charsets.ISO_8859_1)) else String(b, Charsets.ISO_8859_1)
    }

    private fun unsync(b: ByteArray): ByteArray {
        val out = java.io.ByteArrayOutputStream(b.size)
        var i = 0
        while (i < b.size) {
            out.write(b[i].toInt())
            if (b[i] == 0xFF.toByte() && i + 1 < b.size && b[i + 1] == 0.toByte()) i++
            i++
        }
        return out.toByteArray()
    }

    private fun syncsafe(b: ByteArray, o: Int) =
        ((b[o].toInt() and 0x7f) shl 21) or ((b[o + 1].toInt() and 0x7f) shl 14) or ((b[o + 2].toInt() and 0x7f) shl 7) or (b[o + 3].toInt() and 0x7f)

    private fun be32(b: ByteArray, o: Int) =
        ((b[o].toInt() and 0xff) shl 24) or ((b[o + 1].toInt() and 0xff) shl 16) or ((b[o + 2].toInt() and 0xff) shl 8) or (b[o + 3].toInt() and 0xff)

    private fun le32(b: ByteArray, o: Int) =
        (b[o].toInt() and 0xff) or ((b[o + 1].toInt() and 0xff) shl 8) or ((b[o + 2].toInt() and 0xff) shl 16) or ((b[o + 3].toInt() and 0xff) shl 24)

    private fun flac(s: InputStream): String? {
        readUpTo(s, 4)
        while (true) {
            val h = readUpTo(s, 4)
            if (h.size < 4) return null
            val last = h[0].toInt() and 0x80 != 0
            val type = h[0].toInt() and 0x7f
            val len = ((h[1].toInt() and 0xff) shl 16) or ((h[2].toInt() and 0xff) shl 8) or (h[3].toInt() and 0xff)
            if (type == 4) return vorbisComments(readUpTo(s, len), 0)
            var skip = len.toLong()
            while (skip > 0) { val r = s.skip(skip); if (r <= 0) return null; skip -= r }
            if (last) return null
        }
    }

    private fun vorbisComments(b: ByteArray, start: Int): String? {
        try {
            var p = start
            val vlen = le32(b, p); p += 4 + vlen
            val n = le32(b, p); p += 4
            var plain: String? = null
            for (i in 0 until n) {
                if (p + 4 > b.size) break
                val l = le32(b, p); p += 4
                if (l < 0 || p + l > b.size) break
                val c = String(b, p, l, Charsets.UTF_8); p += l
                val eq = c.indexOf('=')
                if (eq <= 0) continue
                val k = c.substring(0, eq).uppercase()
                val v = c.substring(eq + 1)
                if (k == "SYNCEDLYRICS" || (k == "LYRICS" && isSynced(v))) return v
                if (k == "LYRICS" || k == "UNSYNCEDLYRICS") plain = v
            }
            return plain
        } catch (e: Exception) { return null }
    }

    private fun vorbisScan(b: ByteArray): String? {
        // find the comment header packet ("\u0003vorbis" or "OpusTags") and parse from there
        val markers = listOf("\u0003vorbis".toByteArray(Charsets.ISO_8859_1), "OpusTags".toByteArray(Charsets.ISO_8859_1))
        for (m in markers) {
            val i = indexOf(b, m, 0)
            if (i >= 0) return vorbisComments(b, i + m.size)
        }
        return null
    }

    private fun mp4Lyr(b: ByteArray): String? {
        val marker = byteArrayOf(0xA9.toByte(), 'l'.code.toByte(), 'y'.code.toByte(), 'r'.code.toByte())
        var from = 0
        while (true) {
            val i = indexOf(b, marker, from)
            if (i < 0) return null
            // [size]©lyr[size]data[type][locale]text
            if (i + 20 < b.size && String(b, i + 8, 4, Charsets.ISO_8859_1) == "data") {
                val dsz = be32(b, i + 4)
                val end = (i + 4 + dsz).coerceAtMost(b.size)
                if (end > i + 20) return String(b, i + 20, end - (i + 20), Charsets.UTF_8)
            }
            from = i + 4
        }
    }

    private fun indexOf(b: ByteArray, m: ByteArray, from: Int): Int {
        outer@ for (i in from..b.size - m.size) {
            for (j in m.indices) if (b[i + j] != m[j]) continue@outer
            return i
        }
        return -1
    }

    // ---------- online ----------
    private fun http(url: String): Pair<Int, String> {
        val r = Online.get(url, mapOf("User-Agent" to UA, "Lrclib-Client" to UA), 10000, 15000)
        return r.code to r.text
    }

    private fun enc(s: String) = URLEncoder.encode(s, "UTF-8")

    private fun lyr(source: String, text: String) = JSONObject().put("source", source).put("text", text).put("synced", isSynced(text))

    private fun fromLrclib(o: JSONObject): JSONObject? {
        val synced = o.optString("syncedLyrics", "").let { if (o.isNull("syncedLyrics")) "" else it }
        val plain = o.optString("plainLyrics", "").let { if (o.isNull("plainLyrics")) "" else it }
        return when {
            synced.isNotBlank() -> JSONObject().put("source", "online").put("text", synced).put("synced", true)
            plain.isNotBlank() -> JSONObject().put("source", "online").put("text", plain).put("synced", false)
            o.optBoolean("instrumental") -> JSONObject().put("source", "online").put("text", "[00:00.00]纯音乐，请欣赏").put("synced", true)
            else -> null
        }
    }

    /** title/artist variants: display names (e.g. Chinese ones the user picked) first, then the file's own tags. */
    private fun queries(raw: Track): List<Pair<String, String>> {
        val out = ArrayList<Pair<String, String>>()
        for (t in listOf(Library.display(raw), raw).distinct()) {
            var title = t.title.trim(); var artist = t.artist
            if (artist == "未知歌手") {
                artist = ""
                val parts = title.split(" - ", "－", " — ").map { it.trim() }.filter { it.isNotEmpty() }
                if (parts.size >= 2) { out.add(parts[1] to parts[0]); out.add(parts[0] to parts[1]) }
            }
            out.add(title to artist)
            val stripped = title.replace(Regex("[(（\\[【].*?[)）\\]】]"), "").replace(Regex("(?i)\\s*-\\s*(live|remaster(ed)?.*)$"), "").trim()
            val firstArtist = artist.split("/", "、", ",", "，", "&", ";", " feat", " ft.").first().trim()
            if (stripped.isNotEmpty() && (stripped != title || firstArtist != artist)) out.add(stripped to firstArtist)
        }
        return out.distinct()
    }

    private fun norm(s: String) = s.lowercase().replace(Regex("[(（\\[【].*?[)）\\]】]"), "").replace(Regex("[\\s\\p{P}\\p{S}]"), "")
    private fun sameTitle(a: String, b: String): Boolean {
        val x = norm(a); val y = norm(b)
        if (x.isEmpty() || y.isEmpty()) return false
        return x == y || (x.length >= 2 && y.contains(x)) || (y.length >= 2 && x.contains(y))
    }
    private fun durOk(candMs: Long, trackMs: Long, tol: Long = 3500) = trackMs <= 0 || candMs <= 0 || Math.abs(candMs - trackMs) <= tol

    private fun entities(s: String) = s.replace("&apos;", "'").replace("&quot;", "\"").replace("&#39;", "'").replace("&#58;", ":")
        .replace("&#40;", "(").replace("&#41;", ")").replace("&#45;", "-").replace("&#32;", " ").replace("&#10;", "\n").replace("&#13;", "")
        .replace("&lt;", "<").replace("&gt;", ">").replace("&amp;", "&")

    // QQ音乐
    private class Cand(val src: String, val id: String, val title: String, val artist: String, val album: String, val durMs: Long, val extra: String = "")

    private fun qqSearch(q: String, n: Int = 8): List<Cand> {
        val r = Online.get("https://c.y.qq.com/soso/fcgi-bin/client_search_cp?w=${enc(q)}&format=json&p=1&n=$n&cr=1&new_json=0",
            mapOf("Referer" to "https://y.qq.com/"))
        if (r.code != 200) return emptyList()
        val list = JSONObject(r.text.trim().removePrefix("callback(").removeSuffix(")")).optJSONObject("data")?.optJSONObject("song")?.optJSONArray("list") ?: return emptyList()
        return (0 until list.length()).map { list.getJSONObject(it) }.map { o ->
            val singers = o.optJSONArray("singer")
            val ar = if (singers == null) "" else (0 until singers.length()).joinToString("/") { singers.getJSONObject(it).optString("name") }
            Cand("qq", o.optString("songmid"), entities(o.optString("songname")), entities(ar), entities(o.optString("albumname")), o.optLong("interval") * 1000)
        }.filter { it.id.isNotEmpty() }
    }

    private fun qqLyric(mid: String): String? {
        val r = Online.get("https://c.y.qq.com/lyric/fcgi-bin/fcg_query_lyric_new.fcg?songmid=${enc(mid)}&format=json&nobase64=1&g_tk=5381",
            mapOf("Referer" to "https://y.qq.com/portal/player.html"))
        if (r.code != 200) return null
        val o = JSONObject(r.text.trim().removePrefix("MusicJsonCallback(").removeSuffix(")"))
        val l = entities(o.optString("lyric")).trim()
        return l.ifBlank { null }
    }

    // 酷狗
    private fun kugouSearch(keyword: String, durMs: Long): List<Cand> {
        val r = Online.get("https://lyrics.kugou.com/search?ver=1&man=yes&client=pc&keyword=${enc(keyword)}&duration=${if (durMs > 0) durMs else ""}&hash=")
        if (r.code != 200) return emptyList()
        val arr = JSONObject(r.text).optJSONArray("candidates") ?: return emptyList()
        return (0 until arr.length()).map { arr.getJSONObject(it) }.map { o ->
            Cand("kugou", o.optString("id"), o.optString("song"), o.optString("singer"), "", o.optLong("duration"), o.optString("accesskey"))
        }.filter { it.id.isNotEmpty() }
    }

    private fun kugouLyric(c: Cand): String? {
        val r = Online.get("https://lyrics.kugou.com/download?ver=1&client=pc&id=${enc(c.id)}&accesskey=${enc(c.extra)}&fmt=lrc&charset=utf8")
        if (r.code != 200) return null
        val content = JSONObject(r.text).optString("content")
        if (content.isEmpty()) return null
        val text = decodeText(android.util.Base64.decode(content, android.util.Base64.DEFAULT)).trim()
        return text.ifBlank { null }
    }

    // 网易云
    private fun neteaseSearch(q: String, n: Int = 8): List<Cand> {
        val r = Online.get("https://music.163.com/api/cloudsearch/pc?s=${enc(q)}&type=1&limit=$n&offset=0",
            mapOf("Referer" to "https://music.163.com/"))
        if (r.code != 200) return emptyList()
        val songs = JSONObject(r.text).optJSONObject("result")?.optJSONArray("songs") ?: return emptyList()
        return (0 until songs.length()).map { songs.getJSONObject(it) }.map { o ->
            val ars = o.optJSONArray("ar")
            val ar = if (ars == null) "" else (0 until ars.length()).joinToString("/") { ars.getJSONObject(it).optString("name") }
            Cand("netease", o.optLong("id").toString(), o.optString("name"), ar, o.optJSONObject("al")?.optString("name") ?: "", o.optLong("dt"))
        }
    }

    private fun neteaseLyric(id: String): String? {
        val r = Online.get("https://music.163.com/api/song/lyric?id=${enc(id)}&lv=1&tv=-1", mapOf("Referer" to "https://music.163.com/"))
        if (r.code != 200) return null
        val l = JSONObject(r.text).optJSONObject("lrc")?.optString("lyric")?.trim() ?: return null
        return l.ifBlank { null }
    }

    private fun lyricOf(c: Cand): String? = try {
        when (c.src) { "qq" -> qqLyric(c.id); "kugou" -> kugouLyric(c); "netease" -> neteaseLyric(c.id); else -> null }
    } catch (e: Exception) { Log.w(TAG, "lyric ${c.src}", e); null }

    private val SRC_LABEL = mapOf("qq" to "qq", "kugou" to "kugou", "netease" to "netease")

    fun fetchOnline(t: Track): JSONObject? {
        val durMs = t.duration
        var plain: JSONObject? = null
        val tried = HashSet<String>()
        fun consider(c: Cand): JSONObject? {
            if (!tried.add(c.src + c.id)) return null
            val text = lyricOf(c) ?: return null
            val r = lyr(SRC_LABEL[c.src] ?: "online", text)
            if (r.optBoolean("synced")) return r
            if (plain == null) plain = r
            return null
        }
        for ((title, artist) in queries(t)) {
            val q = listOf(title, artist).filter { it.isNotBlank() }.joinToString(" ")
            // 1. QQ音乐（华语歌最全）
            try {
                for (c in qqSearch(q).filter { sameTitle(it.title, title) && durOk(it.durMs, durMs) }.take(2)) consider(c)?.let { return it }
            } catch (e: Exception) { Log.w(TAG, "qq", e) }
            // 2. 酷狗（按时长匹配）
            try {
                val kw = if (artist.isNotBlank()) "$artist - $title" else title
                for (c in kugouSearch(kw, durMs).filter { durOk(it.durMs, durMs, 3000) }.take(2)) consider(c)?.let { return it }
            } catch (e: Exception) { Log.w(TAG, "kugou", e) }
            // 3. 网易云
            try {
                for (c in neteaseSearch(q).filter { sameTitle(it.title, title) && durOk(it.durMs, durMs) }.take(2)) consider(c)?.let { return it }
            } catch (e: Exception) { Log.w(TAG, "netease", e) }
            // 4. LRCLIB
            try {
                lrclib(title, artist, t)?.let { r -> if (r.optBoolean("synced")) return r else if (plain == null) plain = r }
            } catch (e: Exception) { Log.w(TAG, "lrclib", e) }
        }
        return plain
    }

    private fun lrclib(title: String, artist: String, t: Track): JSONObject? {
        val durS = (t.duration / 1000.0)
        if (artist.isNotEmpty()) {
            val url = "https://lrclib.net/api/get?track_name=${enc(title)}&artist_name=${enc(artist)}" +
                (if (durS > 0) "&duration=${Math.round(durS)}" else "")
            val (code, body) = http(url)
            if (code == 200) fromLrclib(JSONObject(body))?.let { if (it.optBoolean("synced")) return it }
        }
        val sUrl = "https://lrclib.net/api/search?track_name=${enc(title)}" + (if (artist.isNotEmpty()) "&artist_name=${enc(artist)}" else "")
        val (sc, sb) = http(sUrl)
        if (sc == 200) return pickBest(JSONArray(sb), durS)
        return null
    }

    private fun pickBest(arr: JSONArray, durS: Double): JSONObject? {
        var best: JSONObject? = null; var bestScore = Double.MAX_VALUE
        for (i in 0 until arr.length()) {
            val o = arr.getJSONObject(i)
            val r = fromLrclib(o) ?: continue
            val d = o.optDouble("duration", 0.0)
            val diff = if (durS > 0 && d > 0) Math.abs(d - durS) else 5.0
            if (durS > 0 && d > 0 && diff > 10) continue
            val score = diff + (if (r.optBoolean("synced")) 0.0 else 6.0)
            if (score < bestScore) { bestScore = score; best = r }
        }
        return best
    }

    private val pool = java.util.concurrent.Executors.newFixedThreadPool(6)

    /** Manual search across QQ音乐 / 酷狗 / 网易云 / LRCLIB: candidates (with lyric text) for the UI. */
    fun search(title: String, artist: String): JSONArray {
        val q = listOf(title, artist).filter { it.isNotBlank() }.joinToString(" ")
        val tasks = ArrayList<java.util.concurrent.Callable<List<JSONObject>>>()
        fun item(src: String, ti: String, ar: String, al: String, durS: Double, text: String) = JSONObject()
            .put("src", src).put("title", ti).put("artist", ar).put("album", al).put("duration", durS)
            .put("synced", isSynced(text)).put("text", text)
        tasks.add(java.util.concurrent.Callable {
            qqSearch(q, 6).take(5).mapNotNull { c -> lyricOf(c)?.let { item("QQ音乐", c.title, c.artist, c.album, c.durMs / 1000.0, it) } }
        })
        tasks.add(java.util.concurrent.Callable {
            kugouSearch(if (artist.isNotBlank()) "$artist - $title" else title, 0).take(5).mapNotNull { c -> lyricOf(c)?.let { item("酷狗", c.title, c.artist, "", c.durMs / 1000.0, it) } }
        })
        tasks.add(java.util.concurrent.Callable {
            neteaseSearch(q, 6).take(5).mapNotNull { c -> lyricOf(c)?.let { item("网易云", c.title, c.artist, c.album, c.durMs / 1000.0, it) } }
        })
        tasks.add(java.util.concurrent.Callable {
            val url = if (artist.isBlank()) "https://lrclib.net/api/search?q=${enc(title)}"
                      else "https://lrclib.net/api/search?track_name=${enc(title)}&artist_name=${enc(artist)}"
            val (code, body) = http(url)
            if (code != 200) emptyList() else {
                val arr = JSONArray(body)
                (0 until minOf(arr.length(), 15)).mapNotNull { i ->
                    val o = arr.getJSONObject(i)
                    val synced = if (o.isNull("syncedLyrics")) "" else o.optString("syncedLyrics")
                    val plain = if (o.isNull("plainLyrics")) "" else o.optString("plainLyrics")
                    if (synced.isBlank() && plain.isBlank()) null
                    else item("LRCLIB", o.optString("trackName"), o.optString("artistName"), o.optString("albumName"), o.optDouble("duration", 0.0), synced.ifBlank { plain })
                }
            }
        })
        val futures = tasks.map { pool.submit(it) }
        val out = JSONArray(); var errors = 0
        for (f in futures) {
            try { f.get(20, java.util.concurrent.TimeUnit.SECONDS).forEach { out.put(it) } }
            catch (e: Exception) { errors++; f.cancel(true) }
        }
        if (out.length() == 0 && errors == futures.size) throw RuntimeException("网络错误")
        return out
    }
}
