package com.sonkoz.glide

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.net.VpnService
import android.os.Build
import android.os.HandlerThread
import android.os.ParcelFileDescriptor
import glide.Glide
import glide.Logger
import glide.Options
import glide.Tunnel
import java.util.TimeZone

class GlideVpnService : VpnService() {

    private var tun: ParcelFileDescriptor? = null
    @Volatile private var tunnel: Tunnel? = null
    private val worker = HandlerThread("glide-lifecycle").apply { start() }
    private val lifecycle by lazy { android.os.Handler(worker.looper) }

    companion object {
        const val ACTION_START = "com.sonkoz.glide.START"
        const val ACTION_STOP = "com.sonkoz.glide.STOP"
        private const val CHANNEL_ID = "glide_vpn"
        private const val NOTIFICATION_ID = 1

        private const val DNS_V4 = "10.111.0.53"
        private const val DNS_V6 = "fd00:5100:6b6f::53"
        private const val ADDR_V4 = "10.111.0.1"
        private const val ADDR_V6 = "fd00:5100:6b6f::1"
        private const val MTU = 1500
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_STOP -> {
                stop()
                return START_NOT_STICKY
            }
            else -> start()
        }
        return START_STICKY
    }

    private fun start() {
        if (tunnel != null) return
        GlideState.setPhase(Phase.ACILIYOR)
        startForeground(NOTIFICATION_ID, buildNotification(Phase.ACILIYOR))
        lifecycle.post { bringUp() }
    }

    private fun bringUp() {
        try {
            applyLocalZone()
            val builder = Builder()
                .setSession(getString(R.string.app_name))
                .setMtu(MTU)
                .addAddress(ADDR_V4, 32)
                .addAddress(ADDR_V6, 128)
                .addRoute("0.0.0.0", 0)
                .addRoute("::", 0)
                .addDnsServer(DNS_V4)
                .addDnsServer(DNS_V6)
            try {
                builder.addDisallowedApplication(packageName)
            } catch (_: Exception) {
            }
            val pfd = builder.establish() ?: throw IllegalStateException("tun yok")
            tun = pfd

            val opts = Options()
            opts.mtu = MTU.toLong()
            opts.dnsV4 = DNS_V4
            opts.dnsV6 = DNS_V6
            opts.verifyOnStart = true

            val logger = Logger { line ->
                GlideState.setDetail(line)
                tunnel?.let { GlideState.setStrategy(it.activeStrategy()) }
            }
            tunnel = Glide.start(pfd.detachFd().toLong(), opts, logger)
            GlideState.setStrategy(tunnel?.activeStrategy() ?: "")
            GlideState.setPhase(Phase.ACIK)
            updateNotification(Phase.ACIK)
        } catch (e: Exception) {
            GlideState.setDetail(e.message ?: "hata")
            tunnel = null
            closeTun()
            GlideState.setPhase(Phase.HATA)
            stopForeground(STOP_FOREGROUND_REMOVE)
            stopSelf()
        }
    }

    private fun stop() {
        if (tunnel == null && tun == null) {
            GlideState.setPhase(Phase.KAPALI)
            stopForeground(STOP_FOREGROUND_REMOVE)
            stopSelf()
            return
        }
        GlideState.setPhase(Phase.KAPANIYOR)
        updateNotification(Phase.KAPANIYOR)
        val t = tunnel
        tunnel = null
        lifecycle.post {
            t?.stop()
            closeTun()
            stopForeground(STOP_FOREGROUND_REMOVE)
            stopSelf()
            GlideState.setPhase(Phase.KAPALI)
        }
    }

    private fun closeTun() {
        try {
            tun?.close()
        } catch (_: Exception) {
        }
        tun = null
    }

    override fun onRevoke() {
        stop()
    }

    override fun onDestroy() {
        val t = tunnel
        tunnel = null
        t?.stop()
        closeTun()
        worker.quitSafely()
        super.onDestroy()
    }

    private fun applyLocalZone() {
        try {
            val tz = TimeZone.getDefault()
            val offset = tz.getOffset(System.currentTimeMillis()) / 1000
            Glide.setLocalZone(tz.id, offset.toLong())
        } catch (_: Exception) {
        }
    }

    private fun updateNotification(phase: Phase) {
        val manager = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        manager.notify(NOTIFICATION_ID, buildNotification(phase))
    }

    private fun buildNotification(phase: Phase): Notification {
        val manager = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val channel = NotificationChannel(CHANNEL_ID, getString(R.string.app_name), NotificationManager.IMPORTANCE_LOW)
            channel.setShowBadge(false)
            manager.createNotificationChannel(channel)
        }
        val open = PendingIntent.getActivity(
            this, 0, Intent(this, MainActivity::class.java),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
        )
        val builder = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            Notification.Builder(this, CHANNEL_ID)
        } else {
            @Suppress("DEPRECATION")
            Notification.Builder(this)
        }
        val text = when (phase) {
            Phase.ACILIYOR -> getString(R.string.status_opening)
            Phase.KAPANIYOR -> getString(R.string.status_closing)
            else -> getString(R.string.notif_active)
        }
        builder
            .setContentTitle(getString(R.string.app_name))
            .setContentText(text)
            .setSmallIcon(R.drawable.ic_status)
            .setContentIntent(open)
            .setOngoing(true)
        if (phase == Phase.ACIK) {
            val stopIntent = PendingIntent.getService(
                this, 1,
                Intent(this, GlideVpnService::class.java).setAction(ACTION_STOP),
                PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
            )
            builder.addAction(
                Notification.Action.Builder(
                    null, getString(R.string.notif_stop), stopIntent
                ).build()
            )
        }
        return builder.build()
    }
}
