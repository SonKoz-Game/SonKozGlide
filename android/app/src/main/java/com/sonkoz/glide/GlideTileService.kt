package com.sonkoz.glide

import android.content.Intent
import android.net.VpnService
import android.service.quicksettings.Tile
import android.service.quicksettings.TileService
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.launch

class GlideTileService : TileService() {

    private var scope: CoroutineScope? = null

    override fun onStartListening() {
        super.onStartListening()
        val s = CoroutineScope(Dispatchers.Main + Job())
        scope = s
        s.launch {
            GlideState.phase.collectLatest { render(it) }
        }
    }

    override fun onStopListening() {
        scope?.cancel()
        scope = null
        super.onStopListening()
    }

    override fun onClick() {
        super.onClick()
        when (GlideState.phase.value) {
            Phase.ACIK, Phase.HATA -> {
                startService(
                    Intent(this, GlideVpnService::class.java).setAction(GlideVpnService.ACTION_STOP)
                )
            }
            Phase.KAPALI -> {
                val prepare = VpnService.prepare(this)
                if (prepare != null) {
                    val activity = Intent(this, MainActivity::class.java)
                        .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                    startActivityAndCollapse(activity)
                } else {
                    startForegroundService(
                        Intent(this, GlideVpnService::class.java).setAction(GlideVpnService.ACTION_START)
                    )
                }
            }
            else -> {}
        }
    }

    private fun render(phase: Phase) {
        val tile = qsTile ?: return
        tile.state = when (phase) {
            Phase.ACIK -> Tile.STATE_ACTIVE
            Phase.ACILIYOR, Phase.KAPANIYOR -> Tile.STATE_UNAVAILABLE
            else -> Tile.STATE_INACTIVE
        }
        tile.label = getString(R.string.app_name)
        tile.updateTile()
    }
}
