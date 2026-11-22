package com.louis.myplayer

import org.json.JSONArray
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL
import java.net.URLEncoder

/** Small HTTP helpers + Apple (iTunes) catalogue lookup used to find Chinese names and cover art. */
object Online {
    const val UA = "Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Mobile Safari/537.36"

    fun enc(s: String): String = URLEncoder.encode(s, "UTF-8")

    class HttpResult(val code: Int, val body: ByteArray) { val text get() = String(body, Charsets.UTF_8) }

    fun get(url: String, headers: Map<String, String> = emptyMap(), connect: Int = 8000, read: Int = 12000): HttpResult {
        val c = URL(url).openConnection() as HttpURLConnection
        c.connectTimeout = connect; c.readTimeout = read
        c.instanceFollowRedirects = true
        c.setRequestProperty("User-Agent", UA)
        headers.forEach { (k, v) -> c.setRequestProperty(k, v) }
        return try {
            val code = c.responseCode
            val body = (if (code in 200..299) c.inputStream else c.errorStream)?.use { it.readBytes() } ?: ByteArray(0)
            HttpResult(code, body)
        } finally { c.disconnect() }
    }

    fun hasCjk(s: String) = s.any { it in '\u4e00'..'\u9fff' || it in '\u3400'..'\u4dbf' }

    // ---- iTunes: ~20 requests/minute per IP, so keep a gap and back off on 429 ----
    private val lock = Object()
    @Volatile private var lastCall = 0L
    private const val GAP_MS = 3200L

    private fun itunes(url: String): JSONArray {
        for (attempt in 0..1) {
            synchronized(lock) {
                val wait = lastCall + GAP_MS - System.currentTimeMillis()
                if (wait > 0) Thread.sleep(wait)
                lastCall = System.currentTimeMillis()
            }
            val r = get(url)
            if (r.code == 200) return JSONObject(r.text).optJSONArray("results") ?: JSONArray()
            if (r.code == 403 || r.code == 429) {
                if (attempt == 0) { synchronized(lock) { lastCall = System.currentTimeMillis() + 45_000 }; continue }
                throw RuntimeException("查询太频繁，请稍后再试")
            }
            throw RuntimeException("HTTP ${r.code}")
        }
        return JSONArray()
    }

    /**
     * Apple's Hong Kong / Taiwan stores index the English names of Chinese songs but return the Chinese ones,
     * so "A Dandelion's Promise Jay Chou" finds 蒲公英的約定 / 周杰倫 / 我很忙 (+ cover art).
     */
    fun candidates(title: String, artist: String, durationMs: Long): JSONArray {
        val terms = ArrayList<String>()
        val a = if (artist == "未知歌手") "" else artist
        terms.add(listOf(title, a).filter { it.isNotBlank() }.joinToString(" "))
        val stripped = title.replace(Regex("[(（\\[【].*?[)）\\]】]"), "").trim()
        if (stripped.isNotEmpty() && stripped != title) terms.add(listOf(stripped, a).filter { it.isNotBlank() }.joinToString(" "))
        val out = JSONArray(); val seen = HashSet<String>()
        loop@ for (term in terms.distinct()) {
            for (country in listOf("hk", "tw")) {
                val arr = itunes("https://itunes.apple.com/search?term=${enc(term)}&entity=song&limit=10&country=$country&lang=en_us")
                for (i in 0 until arr.length()) {
                    val r = arr.getJSONObject(i)
                    val o = JSONObject()
                        .put("title", r.optString("trackName")).put("artist", r.optString("artistName"))
                        .put("album", r.optString("collectionName")).put("duration", r.optLong("trackTimeMillis"))
                        .put("art", r.optString("artworkUrl100").replace("100x100bb", "600x600bb"))
                    val k = o.getString("title") + "|" + o.getString("artist") + "|" + o.getString("album")
                    if (seen.add(k)) out.put(o)
                }
                if (out.length() > 0 && bestIndex(out, title, durationMs) >= 0) break@loop
            }
        }
        return out
    }

    private val liveRe = Regex("(?i)\\blive\\b|現場|现场|演唱會|演唱会|piano|cover|伴奏|instrumental|karaoke|remix|\\bdj\\b")

    /** Index of the confident Chinese match (duration within 2.5 s, same live/cover-ness), or -1. */
    fun bestIndex(arr: JSONArray, title: String, durationMs: Long): Int {
        var best = -1; var bestDiff = Long.MAX_VALUE
        val wantLive = liveRe.containsMatchIn(title)
        for (i in 0 until arr.length()) {
            val o = arr.getJSONObject(i)
            val name = o.optString("title"); val ar = o.optString("artist")
            if (!hasCjk(name) && !hasCjk(ar)) continue
            if (liveRe.containsMatchIn(name + " " + o.optString("album")) != wantLive) continue
            val d = o.optLong("duration")
            if (durationMs <= 0 || d <= 0) continue
            val diff = Math.abs(d - durationMs)
            if (diff <= 2500 && diff < bestDiff) { best = i; bestDiff = diff }
        }
        return best
    }
}
