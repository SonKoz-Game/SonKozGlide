package com.sonkoz.glide

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update

enum class Phase { KAPALI, ACILIYOR, ACIK, KAPANIYOR, HATA }

object GlideState {
    private const val LOG_LIMIT = 40

    private val _phase = MutableStateFlow(Phase.KAPALI)
    val phase: StateFlow<Phase> = _phase.asStateFlow()

    private val _detail = MutableStateFlow("")
    val detail: StateFlow<String> = _detail.asStateFlow()

    private val _strategy = MutableStateFlow("")
    val strategy: StateFlow<String> = _strategy.asStateFlow()

    private val _since = MutableStateFlow(0L)
    val since: StateFlow<Long> = _since.asStateFlow()

    private val _log = MutableStateFlow<List<String>>(emptyList())
    val log: StateFlow<List<String>> = _log.asStateFlow()

    fun setPhase(p: Phase) {
        if (_phase.value == p) return
        _phase.value = p
        _since.value = if (p == Phase.ACIK) System.currentTimeMillis() else 0L
    }

    fun setDetail(line: String) {
        _detail.value = line
        if (line.isNotEmpty()) {
            _log.update { (it + line).takeLast(LOG_LIMIT) }
        }
    }

    fun setStrategy(id: String) {
        _strategy.value = id
    }
}
