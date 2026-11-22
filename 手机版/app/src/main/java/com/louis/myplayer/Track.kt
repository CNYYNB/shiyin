package com.louis.myplayer

import org.json.JSONObject

data class Track(
    val id: String,
    val uri: String,
    val title: String,
    val artist: String,
    val album: String,
    val albumId: Long,
    val duration: Long,
    val path: String,      // absolute file path when known ("" for SAF documents)
    val folder: String,    // human readable folder
    val dateAdded: Long,
    val trackNo: Int,
    val size: Long,
    val lrcUri: String,    // sibling .lrc found during a folder scan ("" if none)
    val fileName: String = "",
) {
    /** Stable identity that survives moving the file / MediaStore re-indexing: file name (minus " (2)" copies) + size. */
    val key: String get() {
        val n = fileName.ifEmpty { path.substringAfterLast('/') }.ifEmpty { title }
        val base = n.substringBeforeLast('.').replace(Regex("\\s*\\(\\d+\\)$"), "").trim().lowercase()
        return base + "|" + size
    }

    fun toJson(): JSONObject = JSONObject().apply {
        put("id", id); put("uri", uri); put("title", title); put("artist", artist)
        put("album", album); put("albumId", albumId); put("duration", duration)
        put("path", path); put("folder", folder); put("dateAdded", dateAdded)
        put("trackNo", trackNo); put("size", size); put("lrcUri", lrcUri); put("fileName", fileName)
    }

    companion object {
        fun fromJson(o: JSONObject) = Track(
            o.optString("id"), o.optString("uri"), o.optString("title"), o.optString("artist"),
            o.optString("album"), o.optLong("albumId"), o.optLong("duration"), o.optString("path"),
            o.optString("folder"), o.optLong("dateAdded"), o.optInt("trackNo"), o.optLong("size"),
            o.optString("lrcUri"), o.optString("fileName"),
        )
    }
}
