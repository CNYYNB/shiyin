package com.louis.myplayer

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.net.wifi.WifiManager
import android.os.Build
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.os.PowerManager
import android.util.Log
import androidx.core.app.NotificationCompat
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat

/**
 * 「传到电脑」前台服务 (2.2)：把 [SyncServer]（电脑连手机）和 [ReverseClient]（手机连电脑）放在前台服务里，
 * 切到后台 / 锁屏后 HyperOS 不会冻结进程；持有高性能 WifiLock + 部分唤醒锁；空闲 30 分钟自动停止。
 */
class SyncService : Service() {
    companion object {
        private const val TAG = "SyncService"
        private const val CHANNEL = "sync"
        private const val NOTIF_ID = 18520
        const val ACTION_START = "com.louis.myplayer.SYNC_START"
        const val ACTION_STOP = "com.louis.myplayer.SYNC_STOP"
        const val IDLE_LIMIT_MS = 30 * 60 * 1000L
        @Volatile var running = false; private set
        /** Set when the service stopped itself (idle / 停止 in the notification), for the UI. */
        @Volatile var stopReason = ""

        fun start(ctx: Context) {
            val i = Intent(ctx, SyncService::class.java).setAction(ACTION_START)
            try { ContextCompat.startForegroundService(ctx, i) } catch (e: Exception) { Log.w(TAG, "start", e) }
        }

        fun stop(ctx: Context) {
            ReverseClient.stop()
            SyncServer.stop()
            try { ctx.stopService(Intent(ctx, SyncService::class.java)) } catch (_: Exception) {}
        }
    }

    private val main = Handler(Looper.getMainLooper())
    private var wifiLock: WifiManager.WifiLock? = null
    private var wakeLock: PowerManager.WakeLock? = null

    private val idleCheck = object : Runnable {
        override fun run() {
            if (!running) return
            val idle = System.currentTimeMillis() - SyncServer.lastActivity
            if (idle >= IDLE_LIMIT_MS) {
                stopReason = "空闲 30 分钟，已自动停止"
                stopAll(); return
            }
            updateNotification()
            main.postDelayed(this, 30_000)
        }
    }

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == ACTION_STOP) {
            stopReason = "已在通知栏停止"
            stopAll(); return START_NOT_STICKY
        }
        val n = buildNotification()
        try {
            ServiceCompat.startForeground(this, NOTIF_ID, n,
                if (Build.VERSION.SDK_INT >= 29) ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC else 0)
        } catch (e: Exception) {
            Log.w(TAG, "startForeground", e) // e.g. started from background: keep running in-process anyway
        }
        if (!SyncServer.running) SyncServer.start(this)
        SyncServer.touch()
        stopReason = ""
        if (!running) {
            running = true
            acquireLocks()
            main.removeCallbacks(idleCheck)
            main.postDelayed(idleCheck, 30_000)
        }
        return START_NOT_STICKY
    }

    private fun stopAll() {
        ReverseClient.stop()
        SyncServer.stop()
        try { ServiceCompat.stopForeground(this, ServiceCompat.STOP_FOREGROUND_REMOVE) } catch (_: Exception) {}
        stopSelf()
    }

    override fun onDestroy() {
        running = false
        main.removeCallbacks(idleCheck)
        ReverseClient.stop()
        SyncServer.stop()
        releaseLocks()
        super.onDestroy()
    }

    @Suppress("DEPRECATION")
    private fun acquireLocks() {
        try {
            val wm = applicationContext.getSystemService(Context.WIFI_SERVICE) as WifiManager
            wifiLock = wm.createWifiLock(WifiManager.WIFI_MODE_FULL_HIGH_PERF, "shiyin:sync").apply { setReferenceCounted(false); acquire() }
        } catch (e: Exception) { Log.w(TAG, "wifilock", e) }
        try {
            val pm = getSystemService(Context.POWER_SERVICE) as PowerManager
            // the idle check stops the service after 30 min; the timeout is only a safety net
            wakeLock = pm.newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "shiyin:sync").apply { setReferenceCounted(false); acquire(3 * 60 * 60 * 1000L) }
        } catch (e: Exception) { Log.w(TAG, "wakelock", e) }
    }

    private fun releaseLocks() {
        try { wifiLock?.let { if (it.isHeld) it.release() } } catch (_: Exception) {}
        try { wakeLock?.let { if (it.isHeld) it.release() } } catch (_: Exception) {}
        wifiLock = null; wakeLock = null
    }

    private fun statusText(): String {
        val rev = ReverseClient.statusLine()
        if (rev.isNotEmpty()) return rev
        val ip = SyncServer.addresses().firstOrNull()
        val c = SyncServer.lastClient
        return when {
            c.isNotEmpty() -> "已连接电脑 $c · 已传 ${SyncServer.served.get()} 首"
            ip != null -> "配对码 ${SyncServer.code} · $ip:${SyncServer.port}"
            else -> "配对码 ${SyncServer.code} · 请连接 Wi-Fi"
        }
    }

    private fun buildNotification(): Notification {
        val nm = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        if (Build.VERSION.SDK_INT >= 26 && nm.getNotificationChannel(CHANNEL) == null) {
            nm.createNotificationChannel(NotificationChannel(CHANNEL, "传到电脑", NotificationManager.IMPORTANCE_LOW).apply {
                description = "传到电脑时显示，保持手机和电脑的连接"; setShowBadge(false)
            })
        }
        val piFlags = PendingIntent.FLAG_UPDATE_CURRENT or (if (Build.VERSION.SDK_INT >= 23) PendingIntent.FLAG_IMMUTABLE else 0)
        val open = PendingIntent.getActivity(this, 1, Intent(this, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP), piFlags)
        val stop = PendingIntent.getService(this, 2, Intent(this, SyncService::class.java).setAction(ACTION_STOP), piFlags)
        return NotificationCompat.Builder(this, CHANNEL)
            .setSmallIcon(R.drawable.ic_sync)
            .setContentTitle("拾音 正在等待电脑连接")
            .setContentText(statusText())
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setSilent(true)
            .setCategory(NotificationCompat.CATEGORY_SERVICE)
            .setContentIntent(open)
            .addAction(0, "停止", stop)
            .setForegroundServiceBehavior(NotificationCompat.FOREGROUND_SERVICE_IMMEDIATE)
            .build()
    }

    private fun updateNotification() {
        try { (getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager).notify(NOTIF_ID, buildNotification()) } catch (_: Exception) {}
    }
}
