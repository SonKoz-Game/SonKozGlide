package com.sonkoz.glide

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

enum class Phase { KAPALI, ACILIYOR, ACIK, KAPANIYOR, HATA }

object GlideState {
    private val _phase = MutableStateFlow(Phase.KAPALI)
    val phase: StateFlow<Phase> = _phase.asStateFlow()

    private val _detail = MutableStateFlow("")
    val detail: StateFlow<String> = _detail.asStateFlow()

    private val _strategy = MutableStateFlow("")
    val strategy: StateFlow<String> = _strategy.asStateFlow()

    fun setPhase(p: Phase) {
        _phase.value = p
    }

    fun setDetail(line: String) {
        _detail.value = line
    }

    fun setStrategy(id: String) {
        _strategy.value = id
    }
}
