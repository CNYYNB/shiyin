package com.louis.myplayer

import android.app.PendingIntent
import android.content.Intent
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.net.Uri
import androidx.media3.common.AudioAttributes
import androidx.media3.common.C
import androidx.media3.common.MediaItem
import androidx.media3.common.util.BitmapLoader
import androidx.media3.common.util.UnstableApi
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.session.MediaSession
import androidx.media3.session.MediaSessionService
import com.google.common.util.concurrent.Futures
import com.google.common.util.concurrent.ListenableFuture
import com.google.common.util.concurrent.MoreExecutors
import java.util.concurrent.Executors

@UnstableApi
class PlaybackService : MediaSessionService() {
    private var session: MediaSession? = null

    override fun onCreate() {
        super.onCreate()
        val player = ExoPlayer.Builder(this)
            .setAudioAttributes(AudioAttributes.Builder().setUsage(C.USAGE_MEDIA).setContentType(C.AUDIO_CONTENT_TYPE_MUSIC).build(), true)
            .setHandleAudioBecomingNoisy(true)
            .setWakeMode(C.WAKE_MODE_LOCAL)
            .build()
        val open = PendingIntent.getActivity(
            this, 0, Intent(this, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        session = MediaSession.Builder(this, player)
            .setSessionActivity(open)
            .setBitmapLoader(CoverBitmapLoader())
            .setCallback(object : MediaSession.Callback {
                override fun onAddMediaItems(
                    mediaSession: MediaSession, controller: MediaSession.ControllerInfo, mediaItems: MutableList<MediaItem>,
                ): ListenableFuture<MutableList<MediaItem>> =
                    Futures.immediateFuture(mediaItems.mapNotNull { Library.resolve(it) }.toMutableList())
            })
            .build()
    }

    override fun onGetSession(controllerInfo: MediaSession.ControllerInfo): MediaSession? = session

    override fun onTaskRemoved(rootIntent: Intent?) {
        val p = session?.player
        if (p == null || !p.playWhenReady || p.mediaItemCount == 0) stopSelf()
    }

    override fun onDestroy() {
        session?.run { player.release(); release() }
        session = null
        super.onDestroy()
    }

    /** Loads artwork for the notification / lock screen from our cover cache. */
    private inner class CoverBitmapLoader : BitmapLoader {
        private val exec = MoreExecutors.listeningDecorator(Executors.newSingleThreadExecutor())

        override fun supportsMimeType(mimeType: String): Boolean = true

        override fun decodeBitmap(data: ByteArray): ListenableFuture<Bitmap> = exec.submit<Bitmap> {
            BitmapFactory.decodeByteArray(data, 0, data.size) ?: throw IllegalArgumentException("decode failed")
        }

        override fun loadBitmap(uri: Uri): ListenableFuture<Bitmap> = exec.submit<Bitmap> {
            if (uri.scheme == "myplayer-cover") {
                val id = uri.lastPathSegment ?: throw IllegalArgumentException("no id")
                val t = Library.get(id) ?: throw IllegalArgumentException("unknown track")
                Covers.bitmap(this@PlaybackService, t) ?: throw IllegalStateException("no cover")
            } else {
                contentResolver.openInputStream(uri)?.use { BitmapFactory.decodeStream(it) } ?: throw IllegalStateException("no bitmap")
            }
        }
    }
}
