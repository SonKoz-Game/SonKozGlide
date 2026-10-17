package com.sonkoz.glide

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.net.VpnService
import android.os.Build
import android.os.ParcelFileDescriptor
import glide.Glide
import glide.Logger
import glide.Options
import glide.Tunnel

class GlideVpnService : VpnService() {

    private var tun: ParcelFileDescriptor? = null
    private var tunnel: Tunnel? = null

    companion object {
        const val ACTION_START = "com.sonkoz.glide.START"
        const val ACTION_STOP = "com.sonkoz.glide.STOP"
        private const val CHANNEL_ID = "glide_vpn"
        private const val NOTIFICATION_ID = 1

        private const val DNS_V4 = "198.18.255.254"
        private const val DNS_V6 = "fd61:7363:6b6f:ffff::ffff"
        private const val ADDR_V4 = "198.18.0.1"
        private const val ADDR_V6 = "fd61:7363:6b6f::1"
        private const val ROUTE_V4 = "198.18.0.0"
        private const val ROUTE_V6 = "fd61:7363:6b6f::"
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
        startForeground(NOTIFICATION_ID, buildNotification())
        try {
            val builder = Builder()
                .setSession(getString(R.string.app_name))
                .setMtu(MTU)
                .addAddress(ADDR_V4, 32)
                .addAddress(ADDR_V6, 128)
                .addRoute(ROUTE_V4, 15)
                .addRoute(ROUTE_V6, 48)
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
            opts.dnsFakeV4 = DNS_V4
            opts.dnsFakeV6 = DNS_V6
            opts.verifyOnStart = true

            val logger = Logger { line ->
                GlideState.setDetail(line)
                tunnel?.let { GlideState.setStrategy(it.activeStrategy()) }
            }
            tunnel = Glide.start(pfd.detachFd().toLong(), opts, logger)
            GlideState.setStrategy(tunnel?.activeStrategy() ?: "")
            GlideState.setPhase(Phase.ACIK)
        } catch (e: Exception) {
            GlideState.setDetail(e.message ?: "hata")
            GlideState.setPhase(Phase.HATA)
            stop()
        }
    }

    private fun stop() {
        GlideState.setPhase(Phase.KAPANIYOR)
        tunnel?.stop()
        tunnel = null
        try {
            tun?.close()
        } catch (_: Exception) {
        }
        tun = null
        stopForeground(STOP_FOREGROUND_REMOVE)
        stopSelf()
        GlideState.setPhase(Phase.KAPALI)
    }

    override fun onDestroy() {
        tunnel?.stop()
        tunnel = null
        try {
            tun?.close()
        } catch (_: Exception) {
        }
        super.onDestroy()
    }

    private fun buildNotification(): Notification {
        val manager = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val channel = NotificationChannel(CHANNEL_ID, getString(R.string.app_name), NotificationManager.IMPORTANCE_LOW)
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
        return builder
            .setContentTitle(getString(R.string.app_name))
            .setContentText(getString(R.string.notif_active))
            .setSmallIcon(R.drawable.ic_status)
            .setContentIntent(open)
            .setOngoing(true)
            .build()
    }
}
