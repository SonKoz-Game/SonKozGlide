import {
    StartBypass,
    StopBypass,
    GetState,
    GetAutoStart,
    SetAutoStart,
    GetLogs,
    GetSettings,
    UpdateSettings,
    GetAppVersion,
    GetPendingUpdate,
    GetTuningReport,
    GetBootReport,
    GetConnectTrace,
    RunServiceCheck,
    InstallUpdate,
    RestartApp,
    Uninstall,
    FinishUninstall
} from '../wailsjs/go/main/App';

import { EventsOn } from '../wailsjs/runtime/runtime';

const MIN_BUSY_MS = { connect: 750, disconnect: 420 };
const SETTLE_MS = 380;
const TEXT_SWAP_MS = 150;
const LAYER_CLOSE_MS = 340;
const TOAST_MS = 8000;
const STEP_GAP_MS = 125;
const STEP_GAP_FAST_MS = 55;
const TRACE_MAX_WAIT_MS = 1600;
const TRACE_COLLAPSE_MS = 2600;
const TRACE_EXPANDED_TOP = 40;
const BOOT_FAILSAFE_MS = 7000;
const UNINSTALL_CLOSE_MS = 2400;

const STRATEGY_NAMES = {
    'md5-disorder': 'Kalkan · karışık',
    'md5-fake': 'Kalkan · tekli',
    'autottl-split': 'Akıllı TTL · bölmeli',
    'ttl-fake': 'TTL · tekli',
    'ttl-hostfake': 'TTL · host bölme',
    'md5-split': 'Klasik · bölmeli',
    'md5-disorder2': 'Klasik · karışık',
    'badseq-disorder': 'Eski · karışık',
    'badseq-split': 'Eski · bölmeli',
    'seqovl-split': 'Örtüşmeli bölme',
    'plain-disorder': 'Sade karışık'
};

const SERVICE_META = {
    discord: { name: 'Discord', idle: 'sohbet · medya' },
    roblox: { name: 'Roblox', idle: 'oyun · API' }
};

const UNINSTALL_STEPS = {
    connection: {
        active: 'Bağlantı kapatılıyor', done: 'Bağlantı kapatıldı', skipped: 'Bağlantı zaten kapalı',
        ticker: 'winws.exe · süreç sonlandırılıyor'
    },
    network: {
        active: 'Ağ ayarları geri yükleniyor', done: 'Ağ ayarları geri yüklendi', skipped: 'Ağ ayarları değişmemiş',
        ticker: 'dns_backup.json · net_tuning.json okunuyor'
    },
    autostart: {
        active: 'Başlangıç görevi siliniyor', done: 'Başlangıç görevi silindi', skipped: 'Başlangıç görevi yok',
        warn: 'Başlangıç görevi silinemedi',
        ticker: 'Görev Zamanlayıcı · SonKozGlide'
    },
    driver: {
        active: 'Ağ sürücüsü kaldırılıyor', done: 'Ağ sürücüsü kaldırıldı', skipped: 'Ağ sürücüsü kayıtlı değil',
        'skipped:foreign': 'Ağ sürücüsüne dokunulmadı', 'warn:inuse': 'Ağ sürücüsü durdurulamadı',
        warn: 'Ağ sürücüsü kaldırılamadı',
        ticker: 'WinDivert · hizmet durduruluyor'
    },
    files: {
        active: 'Bileşenler siliniyor', done: 'Bileşenler silindi', skipped: 'Bileşen bulunamadı',
        ticker: 'C:\\ProgramData\\SonKozGlide'
    },
    data: {
        active: 'Ayarlar ve kayıtlar siliniyor', done: 'Ayarlar ve kayıtlar silindi', skipped: 'Kayıtlı ayar yok',
        warn: 'Bazı kayıtlar silinemedi',
        ticker: '%USERPROFILE%\\.sonkoz · WebView2 profili'
    },
    app: {
        active: 'Uygulama dosyası hazırlanıyor', done: 'Uygulama dosyası silinecek', failed: 'Uygulama dosyası silinemedi',
        ticker: 'çıkışta silinecek dosyalar işaretleniyor'
    }
};

function describeUninstall(event) {
    const text = UNINSTALL_STEPS[event.step];
    const items = event.items || [];
    const title = text[event.state + ':' + event.reason] || text[event.state] || text.done;
    let detail = '';

    switch (event.step) {
    case 'connection':
        if (event.state === 'done') {
            detail = items.length > 1 ? `${items.length} süreç durduruldu` : `${items[0] || 'winws'} durduruldu`;
        }
        break;
    case 'network':
        detail = event.state === 'done'
            ? items.map(item => ({ dns: 'DNS', tuning: 'TCP · MTU' })[item]).join(' · ')
            : '';
        break;
    case 'autostart':
        detail = { done: 'Görev Zamanlayıcı', warn: 'elle silin' }[event.state] || '';
        break;
    case 'driver':
        if (event.state === 'skipped') {
            detail = event.reason === 'foreign' ? 'başka uygulamanın' : '';
        } else if (items.length) {
            detail = items.length > 1 ? `${items.length} uygulama kapatıldı` : `${items[0]} kapatıldı`;
        } else if (event.state === 'done') {
            detail = 'WinDivert';
        }
        break;
    case 'files':
        if (event.pending) {
            detail = `${event.pending} dosya kapanınca`;
        } else if (event.files) {
            detail = `${event.files} dosya · ${formatBytes(event.bytes)}`;
        }
        break;
    case 'data':
        detail = items.map(item => ({ settings: 'ayarlar', webview: 'önbellek' })[item]).join(' · ');
        break;
    case 'app':
        detail = event.state === 'done' ? 'kapanınca' : 'elle silin';
        break;
    }
    return { title, detail };
}

const STEP_ICON = `
    <svg viewBox="0 0 16 16" aria-hidden="true">
        <circle class="ring" cx="8" cy="8" r="6.5"></circle>
        <circle class="arc" cx="8" cy="8" r="6.5" pathLength="40"></circle>
        <circle class="disc" cx="8" cy="8" r="7.5"></circle>
        <path class="mark mark-ok" d="M4.8 8.3l2.1 2.1 4.3-4.6" pathLength="12"></path>
        <path class="mark mark-fail" d="M5.6 5.6l4.8 4.8M10.4 5.6l-4.8 4.8" pathLength="12"></path>
        <path class="mark mark-warn" d="M8 4.4v4.6M8 11.4v.2" pathLength="12"></path>
        <path class="mark mark-skip" d="M5.2 8h5.6" pathLength="12"></path>
    </svg>`;

const wait = (ms) => new Promise(resolve => setTimeout(resolve, ms));

function strategyName(id) {
    return STRATEGY_NAMES[id] || id || 'Bağlantı modu';
}

function formatSeconds(ms) {
    return (Math.max(0, ms) / 1000).toFixed(ms < 10000 ? 2 : 1).replace('.', ',') + ' sn';
}

function formatShortSeconds(ms) {
    return (Math.max(0, ms) / 1000).toFixed(1).replace('.', ',') + ' sn';
}

function formatBytes(bytes) {
    if (!bytes) return '0 B';
    if (bytes < 1024) return bytes + ' B';
    if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(0) + ' KB';
    return (bytes / 1024 / 1024).toFixed(1).replace('.', ',') + ' MB';
}

function createStepRow(title, detail) {
    const row = document.createElement('li');
    row.className = 'step is-active';
    row.innerHTML = `<span class="step-icon">${STEP_ICON}</span><span class="step-title"></span><span class="step-detail"></span>`;
    setStepRow(row, 'active', title, detail);
    return row;
}

function setStepRow(row, state, title, detail) {
    row.classList.remove('is-active', 'is-done', 'is-failed', 'is-warn', 'is-skipped');
    row.classList.add('is-' + state);
    row.querySelector('.step-title').textContent = title;
    row.querySelector('.step-detail').textContent = detail || '';
}

document.addEventListener('DOMContentLoaded', () => {
    const byId = (id) => document.getElementById(id);
    const reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;

    const statusIndicator = byId('status-indicator');
    const statusText = byId('status-text');
    const statusCaption = byId('status-caption');
    const powerBtn = byId('power-btn');
    const supportContent = byId('support-content');
    const settingsBtn = byId('settings-btn');
    const settingsPanel = byId('settings-panel');
    const supportViewer = byId('support-viewer');
    const supportScrim = byId('support-scrim');
    const settingsClose = byId('settings-close');
    const supportClose = byId('support-close');
    const versionBadge = byId('version-badge');
    const errorCard = byId('error-card');
    const errorMsg = byId('error-msg');
    const errorClose = byId('error-close');
    const toastTimer = byId('toast-timer');
    const updateCard = byId('update-card');
    const updateLabel = byId('update-label');
    const supportBtn = byId('support-btn');
    const servicesGrid = byId('services-grid');
    const servicesRefresh = byId('services-refresh');
    const uninstallBtn = byId('uninstall-btn');
    const uninstallScrim = byId('uninstall-scrim');
    const uninstallDialog = byId('uninstall-dialog');
    const uninstallCancel = byId('uninstall-cancel');
    const uninstallConfirm = byId('uninstall-confirm');

    const statusPollMs = 5000;
    const supportPollMs = 1800;

    let supportInterval = null;
    let lastSupportHTML = '';
    let animateNextSupportRender = false;
    let lastPhase = 'idle';
    let stateSeq = 0;
    let booting = true;
    let serviceResults = {};
    let servicesBusy = false;
    let uninstalling = false;

    const PHASE_TEXT = {
        idle: {
            text: 'Bağlantı kapalı',
            caption: 'Hazır olduğunda tek dokunuşla başlatabilirsiniz.'
        },
        starting: {
            text: 'Bağlanıyor',
            caption: 'Glide hattınıza en uygun bağlantı modunu ölçüyor.'
        },
        running: {
            text: 'Bağlantı aktif',
            caption: 'Glide bağlantınızı arka planda izliyor.'
        },
        recovering: {
            text: 'Bağlantı korunuyor',
            caption: 'Glide bağlantıyı arka planda tazeliyor, uygulamanızı kapatmanıza gerek yok.'
        },
        stopping: {
            text: 'Kapatılıyor',
            caption: 'Bağlantı güvenli şekilde kapatılıyor.'
        }
    };

    const DETAIL_TEXT = {
        'baglanti hazirlaniyor': 'Bağlantı hazırlanıyor.',
        'baglanti dogrulaniyor': 'Bağlantı doğrulanıyor.',
        'farkli mod deneniyor': 'Hattınızda daha uygun bir bağlantı modu deneniyor.',
        'dogrulama suruyor': 'Bağlantı açık; doğrulama arka planda sürüyor.',
        'kismi baglanti': 'Tam uyumlu mod bulunamadı; en iyi sonuç veren mod kullanılıyor.',
        'baglanti tazeleniyor': 'Bağlantı tazeleniyor.',
        'baglanti yeniden kuruluyor': 'Bağlantı yeniden kuruluyor.',
        'internet baglantisi bekleniyor': 'İnternet bağlantınız kesildi; geri geldiğinde otomatik devam edecek.',
        'mod degistiriliyor': 'Yeni bağlantı modu uygulanıyor.',
        'baglanti kapatiliyor': 'Bağlantı güvenli şekilde kapatılıyor.'
    };

    function finishBooting() {
        if (!booting) return;
        booting = false;
        requestAnimationFrame(() => requestAnimationFrame(() => {
            document.body.classList.remove('booting');
        }));
    }
    setTimeout(finishBooting, 600);

    function openLayer(el) {
        clearTimeout(el._closeTimer);
        el._closeTimer = null;
        if (el.hidden) {
            el.hidden = false;
            void el.offsetWidth;
        }
        el.classList.add('is-open');
    }

    function closeLayer(el) {
        if (el.hidden) return;
        el.classList.remove('is-open');
        clearTimeout(el._closeTimer);
        el._closeTimer = setTimeout(() => {
            el.hidden = true;
            el._closeTimer = null;
        }, LAYER_CLOSE_MS);
    }

    function isOpen(el) {
        return !el.hidden && el.classList.contains('is-open');
    }

    function swapText(el, text, apply) {
        if (el.dataset.text === text) return;
        el.dataset.text = text;
        const commit = apply || (() => { el.textContent = text; });

        clearTimeout(el._swapTimer);
        if (booting) {
            el.classList.remove('is-swapping');
            commit();
            return;
        }
        el.classList.add('is-swapping');
        el._swapTimer = setTimeout(() => {
            commit();
            el.classList.remove('is-swapping');
        }, TEXT_SWAP_MS);
    }

    function morphWidth(el, mutate) {
        const from = el.getBoundingClientRect().width;
        el.style.width = '';
        mutate();
        const to = el.getBoundingClientRect().width;
        if (booting || Math.abs(to - from) < 1) return;

        el.style.width = from + 'px';
        void el.offsetWidth;
        el.style.width = to + 'px';
        clearTimeout(el._widthTimer);
        el._widthTimer = setTimeout(() => { el.style.width = ''; }, 380);
    }

    const trace = {
        el: byId('trace'),
        idle: document.querySelector('#trace .trace-idle'),
        list: byId('trace-steps'),
        summary: byId('trace-summary'),
        summaryText: byId('trace-summary-text'),
        run: 0,
        kind: '',
        queue: [],
        timer: null,
        lastRevealAt: 0,
        rows: new Map(),
        ready: null,
        strategy: null,
        collapseTimer: null
    };

    function setTraceView(view) {
        trace.el.dataset.view = view;
        trace.summary.setAttribute('aria-expanded', String(view === 'expanded'));
        fitTrace();
    }

    function fitTrace() {
        const view = trace.el.dataset.view;
        let height = trace.list.offsetHeight;
        if (view === 'idle') height = trace.idle.offsetHeight;
        if (view === 'summary') height = trace.summary.offsetHeight;
        if (view === 'expanded') height = TRACE_EXPANDED_TOP + trace.list.offsetHeight;
        trace.el.style.setProperty('--trace-h', Math.max(30, Math.ceil(height)) + 'px');
    }

    window.addEventListener('resize', fitTrace);
    if (document.fonts && document.fonts.ready) document.fonts.ready.then(fitTrace);
    fitTrace();

    function traceView() {
        return trace.el.dataset.view;
    }

    function resetTrace(run, kind) {
        clearTimeout(trace.timer);
        clearTimeout(trace.collapseTimer);
        trace.timer = null;
        trace.collapseTimer = null;
        trace.run = run;
        trace.kind = kind || 'connect';
        trace.queue = [];
        trace.rows.clear();
        trace.list.innerHTML = '';
        trace.list.classList.remove('is-overflowing');
        trace.ready = null;
        trace.strategy = null;
        trace.lastRevealAt = 0;
        if (lastPhase !== 'idle') setTraceView('live');
        fitTrace();
    }

    function traceAccept(event, instant) {
        if (!event) return;
        if (event.step === 'run') {
            resetTrace(event.run, event.detail);
            return;
        }
        if (event.run !== trace.run) {
            if (event.run < trace.run) return;
            resetTrace(event.run, 'connect');
        }
        trace.queue.push(event);
        if (instant) {
            while (trace.queue.length) renderTraceEvent(trace.queue.shift());
            onTraceIdle();
            return;
        }
        pumpTrace();
    }

    function pumpTrace() {
        if (trace.timer) return;
        const reveal = () => {
            trace.timer = null;
            const event = trace.queue.shift();
            if (!event) {
                onTraceIdle();
                return;
            }
            renderTraceEvent(event);
            trace.lastRevealAt = performance.now();
            const gap = reduceMotion ? 0 : trace.queue.length > 3 ? STEP_GAP_FAST_MS : STEP_GAP_MS;
            trace.timer = setTimeout(reveal, gap);
        };
        const since = performance.now() - trace.lastRevealAt;
        trace.timer = setTimeout(reveal, reduceMotion ? 0 : Math.max(0, STEP_GAP_MS - since));
    }

    function traceRemainingMs() {
        if (!trace.timer && !trace.queue.length) return 0;
        return Math.min(TRACE_MAX_WAIT_MS, (trace.queue.length + 1) * STEP_GAP_MS);
    }

    function describeTraceEvent(event) {
        const ok = event.ok || 0;
        const count = event.count || 0;
        const ratio = count ? `${ok}/${count}` : '';
        const latency = event.latency ? ` · ${event.latency} ms` : '';

        switch (event.step) {
            case 'prepare':
                return event.state === 'active'
                    ? { state: 'active', title: 'Ağ bileşenleri hazırlanıyor', detail: '' }
                    : { state: 'done', title: 'Ağ bileşenleri hazır', detail: 'sürücü · kurallar' };
            case 'dns':
                if (event.state === 'active') return { state: 'active', title: 'Güvenli DNS uygulanıyor', detail: 'DoH' };
                if (event.detail === 'doh') return { state: 'done', title: 'Güvenli DNS etkin', detail: 'Cloudflare · Google' };
                if (event.detail === 'slow') return { state: 'warn', title: 'Güvenli DNS yavaş', detail: 'tercihlerden kapatılabilir' };
                if (event.detail === 'reverted') return { state: 'warn', title: 'Güvenli DNS geri alındı', detail: 'önceki DNS' };
                if (event.detail === 'off') return { state: 'skipped', title: 'Güvenli DNS kapalı', detail: 'tercihlerden' };
                return { state: 'skipped', title: 'Güvenli DNS bekliyor', detail: 'ağ bekleniyor' };
            case 'tuning':
                if (event.state === 'active') return { state: 'active', title: 'Bağlantı ayarları denetleniyor', detail: 'MTU · TCP' };
                if ((event.detail || '').startsWith('fixed:')) {
                    return { state: 'done', title: 'Bağlantı ayarları iyileştirildi', detail: `${event.detail.slice(6)} düzeltme` };
                }
                if (event.detail === 'ok') return { state: 'done', title: 'Bağlantı ayarları uygun', detail: 'MTU · TCP' };
                if (event.detail === 'off') return { state: 'skipped', title: 'Ayar iyileştirme kapalı', detail: 'tercihlerden' };
                return { state: 'skipped', title: 'Bağlantı ayarları denetlenemedi', detail: '' };
            case 'strategy': {
                const name = strategyName(event.detail);
                if (event.state === 'active') {
                    return { state: 'active', title: 'Bağlantı modu ölçülüyor', detail: `${name} · ${event.index}/${event.total}` };
                }
                if (event.state === 'done') return { state: 'done', title: 'Mod doğrulandı', detail: `${name} · ${ratio}` };
                if (event.state === 'failed') {
                    return count
                        ? { state: 'failed', title: 'Mod elendi', detail: `${name} · ${ratio}` }
                        : { state: 'failed', title: 'Mod başlatılamadı', detail: name };
                }
                return event.index
                    ? { state: 'warn', title: 'Bağlantı modu seçildi', detail: `${name} · doğrulama sürüyor` }
                    : { state: 'warn', title: 'En iyi kısmi mod seçildi', detail: name };
            }
            case 'service': {
                const meta = SERVICE_META[event.detail] || { name: event.detail };
                return event.state === 'done'
                    ? { state: 'done', title: `${meta.name} erişimi doğrulandı`, detail: ratio + latency }
                    : { state: 'warn', title: `${meta.name} erişimi kısmi`, detail: ratio };
            }
            default:
                return null;
        }
    }

    function traceKey(event) {
        if (event.step === 'strategy') return 'strategy:' + (event.index || 'fallback');
        if (event.step === 'service') return 'service:' + event.detail;
        return event.step;
    }

    function renderTraceEvent(event) {
        if (event.step === 'ready') {
            trace.ready = event;
            return;
        }
        if (event.step === 'strategy' && event.state !== 'active' && event.state !== 'failed') {
            trace.strategy = event;
        }
        if (event.step === 'service' && event.state !== 'active') {
            serviceResults[event.detail] = event;
            if (isOpen(supportViewer) && !servicesBusy) renderServices();
        }

        const copy = describeTraceEvent(event);
        if (!copy) return;

        const key = traceKey(event);
        let row = trace.rows.get(key);
        if (!row) {
            row = createStepRow(copy.title, copy.detail);
            trace.rows.set(key, row);
            trace.list.appendChild(row);
        }
        setStepRow(row, copy.state, copy.title, copy.detail);
        row.classList.toggle('is-muted', event.step === 'strategy' && event.state === 'failed');

        const overflowing = trace.list.scrollHeight > trace.list.clientHeight + 2;
        trace.list.classList.toggle('is-overflowing', overflowing);
        if (overflowing) trace.list.scrollTop = trace.list.scrollHeight;
        updateTraceSummary();
        fitTrace();
    }

    function updateTraceSummary() {
        const partial = trace.ready && trace.ready.state !== 'done';
        const checks = Array.from(trace.rows.values()).filter(row => row.classList.contains('is-done')).length;
        const elapsed = trace.ready ? formatShortSeconds(trace.ready.at) : '';

        let text = partial ? `Kısmi bağlantı · ${elapsed}` : `Hazır · ${checks} kontrol · ${elapsed}`;
        if (trace.kind === 'recover' && !partial) text = `Bağlantı tazelendi · ${elapsed}`;

        trace.summaryText.textContent = text;
        trace.summary.classList.toggle('is-warn', Boolean(partial));
    }

    function onTraceIdle() {
        if (!trace.ready || traceView() !== 'live') return;
        clearTimeout(trace.collapseTimer);
        trace.collapseTimer = setTimeout(() => {
            trace.collapseTimer = null;
            if (traceView() === 'live' && lastPhase !== 'idle') {
                updateTraceSummary();
                setTraceView('summary');
            }
        }, reduceMotion ? 800 : TRACE_COLLAPSE_MS);
    }

    trace.summary.addEventListener('click', () => {
        const next = traceView() === 'expanded' ? 'summary' : 'expanded';
        setTraceView(next);
        if (next === 'expanded') trace.list.scrollTop = trace.list.scrollHeight;
    });

    EventsOn('service_progress', (event) => traceAccept(event, false));

    const power = { shown: 'off', busySince: 0, timer: null };

    function applyPower(state, intent) {
        power.shown = state;
        powerBtn.dataset.state = state;
        if (intent) powerBtn.dataset.intent = intent;

        const connecting = powerBtn.dataset.intent !== 'disconnect';
        const checked = state === 'on' || state === 'settling' || (state === 'busy' && connecting);
        powerBtn.setAttribute('aria-checked', String(checked));
        powerBtn.setAttribute('aria-busy', String(state === 'busy' || state === 'settling'));
        document.body.dataset.conn = state === 'on' || state === 'settling' ? 'on' : state === 'off' ? 'off' : 'busy';
    }

    function showPower(target, intent, commit) {
        if (target === 'on' && power.shown === 'settling') {
            commit();
            return;
        }

        clearTimeout(power.timer);
        power.timer = null;

        if (target === 'busy') {
            if (power.shown !== 'busy') power.busySince = performance.now();
            applyPower('busy', intent);
            commit();
            return;
        }

        if (power.shown !== 'busy' || booting) {
            applyPower(target);
            commit();
            return;
        }

        const minBusy = MIN_BUSY_MS[powerBtn.dataset.intent] || MIN_BUSY_MS.connect;
        const busyLeft = power.busySince + minBusy - performance.now();
        const traceLeft = target === 'on' ? traceRemainingMs() : 0;
        power.timer = setTimeout(() => {
            commit();
            if (target !== 'on') {
                applyPower(target);
                power.timer = null;
                return;
            }
            applyPower('settling');
            power.timer = setTimeout(() => {
                applyPower('on');
                power.timer = null;
            }, SETTLE_MS);
        }, Math.max(0, busyLeft, traceLeft));
    }

    function renderState(state) {
        if (!state) return;

        const phase = PHASE_TEXT[state.phase] ? state.phase : 'idle';
        const copy = PHASE_TEXT[phase];
        const caption = DETAIL_TEXT[state.detail] || copy.caption;
        lastPhase = phase;

        if (phase === 'idle' && traceView() !== 'idle') {
            clearTimeout(trace.collapseTimer);
            setTraceView('idle');
        }

        let target = 'busy';
        if (phase === 'idle') target = 'off';
        if (phase === 'running' || phase === 'recovering') target = 'on';

        showPower(target, phase === 'stopping' ? 'disconnect' : 'connect', () => {
            statusIndicator.dataset.phase = phase;
            powerBtn.toggleAttribute('data-recovering', phase === 'recovering');
            swapText(statusText, copy.text, () => {
                morphWidth(statusIndicator, () => { statusText.textContent = copy.text; });
            });
            swapText(statusCaption, caption);
        });

        finishBooting();
    }

    function acceptState(state) {
        stateSeq++;
        renderState(state);
    }

    async function checkStatus() {
        const seq = stateSeq;
        try {
            const state = await GetState();
            if (seq === stateSeq) renderState(state);
        } catch (err) {
            console.warn('Durum okunamadı:', err);
        }
    }

    function normalizeError(message) {
        const value = String(message || '').toLowerCase();

        if (value.includes('locked') || value.includes('already running')) {
            return 'Bağlantı şu anda başka bir işlem tarafından kullanılıyor. Uygulamayı yeniden başlatmayı deneyin.';
        }
        if (value.includes('driver') || value.includes('windivert')) {
            return 'Gerekli ağ bileşeni başlatılamadı. Uygulamayı yönetici olarak çalıştırın.';
        }
        if (value.includes('uygun mod bulunamadi')) {
            return 'Hattınızda çalışan bir bağlantı modu bulunamadı. Birkaç dakika sonra tekrar deneyin.';
        }
        if (value.includes('baslatilamadi') || value.includes('başlatılamadı')) {
            return 'Bağlantı başlatılamadı. Birkaç saniye sonra tekrar deneyin.';
        }
        if (value.includes('timeout')) {
            return 'Bağlantı zaman aşımına uğradı. İnternet bağlantınızı kontrol edip tekrar deneyin.';
        }

        return 'Bağlantı kurulamadı. Lütfen tekrar deneyin.';
    }

    function showMessage(message, type = 'error') {
        if (uninstalling) return;
        errorMsg.textContent = message;
        errorCard.classList.toggle('notice-card', type !== 'error');
        supportBtn.classList.toggle('has-error', type === 'error');
        openLayer(errorCard);

        toastTimer.style.setProperty('--toast-ms', TOAST_MS + 'ms');
        toastTimer.classList.remove('run');
        void toastTimer.offsetWidth;
        toastTimer.classList.add('run');
    }

    function hideMessage() {
        toastTimer.classList.remove('run');
        closeLayer(errorCard);
    }

    toastTimer.addEventListener('animationend', hideMessage);

    function escapeHTML(value) {
        return String(value).replace(/[&<>"']/g, ch => ({
            '&': '&amp;',
            '<': '&lt;',
            '>': '&gt;',
            '"': '&quot;',
            "'": '&#39;'
        }[ch]));
    }

    function supportMessageForLine(line) {
        const lower = line.toLowerCase();

        if (lower.includes('already running') || lower.includes('locked')) {
            return {
                type: 'error',
                title: 'Bağlantı kullanılamıyor',
                detail: 'Glide gerekli ağ kaynağına erişemedi.'
            };
        }
        if (lower.includes('driver') || lower.includes('windivert')) {
            return {
                type: 'error',
                title: 'Ağ bileşeni başlatılamadı',
                detail: 'Uygulamayı yönetici olarak çalıştırmak gerekebilir.'
            };
        }
        if (lower.includes('strategy selected=')) {
            return {
                type: 'ok',
                title: 'Bağlantı modu doğrulandı',
                detail: 'Hattınızda ölçülen en güvenilir mod kullanılıyor.'
            };
        }
        if (lower.includes('rejected:')) {
            return {
                type: 'warning',
                title: 'Bağlantı modu elendi',
                detail: 'Bu mod hattınızda yeterince güvenilir değildi; sıradaki denendi.'
            };
        }
        if (lower.includes('no strategy passed')) {
            return {
                type: 'warning',
                title: 'Tam uyumlu mod bulunamadı',
                detail: 'En iyi sonuç veren mod kullanılıyor; Glide ölçmeye devam ediyor.'
            };
        }
        if (lower.includes('switching profile') || lower.includes('initial health check failed')) {
            return {
                type: 'warning',
                title: 'Daha stabil moda geçiliyor',
                detail: 'Glide bağlantı kalitesini korumak için modu değiştiriyor.'
            };
        }
        if (lower.includes('waiting for internet')) {
            return {
                type: 'warning',
                title: 'İnternet bağlantısı yok',
                detail: 'Bağlantınız geri geldiğinde Glide kaldığı yerden devam edecek.'
            };
        }
        if (lower.includes('keeping service running')) {
            return {
                type: 'info',
                title: 'Bağlantı korunuyor',
                detail: 'Kısa süreli kontrol hatası yeniden başlatmadan izleniyor.'
            };
        }
        if (lower.includes('encrypted dns (doh) applied')) {
            return {
                type: 'ok',
                title: 'Güvenli DNS etkin',
                detail: 'Alan adı sorguları şifreli olarak çözülüyor.'
            };
        }
        if (lower.includes('reverting to the original resolvers')) {
            return {
                type: 'warning',
                title: 'Güvenli DNS geri alındı',
                detail: 'Şifreli DNS yanıt vermedi; önceki DNS ayarlarınız kullanılıyor.'
            };
        }
        if (lower.includes('network interfaces changed')) {
            return {
                type: 'info',
                title: 'Ağ değişikliği algılandı',
                detail: 'Güvenli DNS yeni bağlantıya uygulanıyor.'
            };
        }
        if (lower.includes('health check failed') || lower.includes('restart failed')) {
            return {
                type: 'warning',
                title: 'Bağlantı yeniden deneniyor',
                detail: 'Kısa süre içinde otomatik olarak tekrar bağlanacak.'
            };
        }
        if (lower.includes('adaptive selected')) {
            return {
                type: 'ok',
                title: 'En uygun mod seçildi',
                detail: 'Glide bağlantı için en hızlı çalışan modu kullandı.'
            };
        }
        if (lower.includes('starting strategy') || lower.includes('starting profile')) {
            return {
                type: 'info',
                title: 'Bağlantı modu deneniyor',
                detail: 'Glide modu hattınızda gerçek bağlantılarla ölçüyor.'
            };
        }

        return {
            type: 'info',
            title: 'Bağlantı izleniyor',
            detail: 'Glide bağlantı durumunu arka planda kontrol ediyor.'
        };
    }

    const TUNING_NAMES = {
        'TCP pencere olcekleme': 'TCP pencere ölçekleme',
        'MTU': 'Paket boyutu (MTU)',
        'Guvenli DNS': 'Güvenli DNS'
    };

    const TUNING_STATUS = {
        ok: { type: 'ok', label: 'Sorun yok' },
        fixed: { type: 'ok', label: 'Düzeltildi' },
        skipped: { type: 'info', label: 'Kontrol edilemedi' }
    };

    function supportRow(type, title, detail) {
        return `
            <div class="support-row support-${type}">
                <span class="support-dot"></span>
                <div>
                    <strong>${escapeHTML(title)}</strong>
                    <small>${escapeHTML(detail)}</small>
                </div>
            </div>
        `;
    }

    function renderTuningReport(report) {
        const findings = (report && report.findings) || [];
        const rows = [];

        if (trace.strategy && lastPhase !== 'idle') {
            const s = trace.strategy;
            const verified = s.state === 'done';
            rows.push(supportRow(
                verified ? 'ok' : 'warning',
                `Bağlantı modu · ${strategyName(s.detail)}`,
                verified
                    ? `Hattınızda ölçüldü: ${s.ok}/${s.count} el sıkışma başarılı`
                    : 'Doğrulama arka planda sürüyor'
            ));
        }

        findings.forEach(item => {
            const status = TUNING_STATUS[item.status] || TUNING_STATUS.skipped;
            const name = TUNING_NAMES[item.name] || item.name;
            rows.push(supportRow(status.type, `${name} · ${status.label}`, item.detail));
        });

        return rows.length ? `<div class="support-section">Bağlantı Doktoru</div>${rows.join('')}` : '';
    }

    function renderSupportInfo(logs, tuning) {
        const tuningRows = renderTuningReport(tuning);

        const logRows = (logs || '')
            .split('\n')
            .map(line => line.trim())
            .filter(Boolean)
            .slice(-8)
            .map(line => supportMessageForLine(line))
            .map(item => supportRow(item.type, item.title, item.detail))
            .join('');

        const html = (tuningRows + (logRows ? `<div class="support-section">Bağlantı durumu</div>${logRows}` : '')) ||
            '<div class="support-empty">Bağlantı bilgisi henüz oluşmadı.</div>';

        if (html === lastSupportHTML) return;
        lastSupportHTML = html;
        supportContent.innerHTML = html;

        if (animateNextSupportRender) {
            animateNextSupportRender = false;
            supportContent.querySelectorAll('.support-row, .support-section').forEach((node, index) => {
                node.style.setProperty('--i', Math.min(index, 12));
                node.classList.add('enter');
            });
        }
        supportContent.scrollTop = supportContent.scrollHeight;
    }

    async function refreshSupportInfo() {
        try {
            const [logs, tuning] = await Promise.all([GetLogs(), GetTuningReport()]);
            renderSupportInfo(logs, tuning);
        } catch (err) {
            lastSupportHTML = '';
            supportContent.innerHTML = '<div class="support-empty">Bağlantı bilgileri okunamadı.</div>';
        }
    }

    function renderServices() {
        servicesGrid.innerHTML = Object.keys(SERVICE_META).map(id => {
            const meta = SERVICE_META[id];
            const result = serviceResults[id];
            let state = 'idle';
            let detail = meta.idle;
            let title = '';

            if (servicesBusy) {
                state = 'busy';
                detail = 'ölçülüyor…';
            } else if (result) {
                const ok = result.ok || 0;
                const count = result.count || 0;
                state = count && ok === count ? 'ok' : ok > 0 ? 'warn' : 'error';
                detail = count ? `${ok}/${count}${result.latency ? ` · ${result.latency} ms` : ''}` : 'yanıt yok';
                if (result.failed && result.failed.length) title = 'Yanıt vermeyen: ' + result.failed.join(', ');
            }

            return `<div class="service-card svc-${state}" title="${escapeHTML(title)}">
                <strong><span class="svc-dot"></span>${escapeHTML(meta.name)}</strong>
                <small>${escapeHTML(detail)}</small>
            </div>`;
        }).join('');
    }

    servicesRefresh.addEventListener('click', async () => {
        if (servicesBusy) return;
        servicesBusy = true;
        servicesRefresh.classList.add('is-busy');
        renderServices();
        try {
            const results = await RunServiceCheck();
            serviceResults = {};
            (results || []).forEach(result => { serviceResults[result.id] = result; });
        } catch (err) {
            showMessage('Servis kontrolü yapılamadı. Lütfen tekrar deneyin.');
        } finally {
            servicesBusy = false;
            servicesRefresh.classList.remove('is-busy');
            renderServices();
        }
    });

    const update = { state: 'idle', version: '' };

    function setUpdateView(state, text, progress) {
        update.state = state;
        updateCard.dataset.state = state;
        updateCard.disabled = state === 'download' || state === 'install' || state === 'restarting';
        updateCard.style.setProperty('--p', String(progress));
        updateLabel.textContent = text;
    }

    function showUpdateAvailable(version) {
        if (!version || update.state !== 'idle') return;
        update.version = version;
        setUpdateView('idle', `Yeni sürüm hazır (${version}) · Güncelle`, 0);
        updateCard.hidden = false;
        versionBadge.classList.add('has-update');
    }

    EventsOn('update_available', showUpdateAvailable);

    EventsOn('update_progress', (progress) => {
        if (update.state !== 'download' && update.state !== 'install') return;
        if (progress.stage === 'install') {
            setUpdateView('install', 'Yeni sürüm kuruluyor…', 1);
            return;
        }
        const total = progress.total || 0;
        const ratio = total ? Math.min(1, progress.downloaded / total) : 0;
        setUpdateView('download', total
            ? `İndiriliyor %${Math.floor(ratio * 100)} · ${formatBytes(progress.downloaded)} / ${formatBytes(total)}`
            : `İndiriliyor · ${formatBytes(progress.downloaded)}`, ratio);
    });

    EventsOn('service_state', acceptState);

    EventsOn('service_error', (message) => {
        showMessage(normalizeError(message));
    });

    GetAppVersion().then(v => {
        versionBadge.textContent = v;
    }).catch(() => {});

    GetPendingUpdate().then(showUpdateAvailable).catch(() => {});

    updateCard.addEventListener('click', async () => {
        if (update.state === 'ready') {
            setUpdateView('restarting', 'Yeniden başlatılıyor…', 1);
            let restarted = false;
            try {
                restarted = await RestartApp() === 'OK';
            } catch (err) {
                restarted = false;
            }
            if (!restarted) {
                setUpdateView('ready', 'Güncelleme hazır · Yeniden başlat', 1);
                showMessage('Glide yeniden başlatılamadı. Uygulamayı kapatıp tekrar açın.');
            }
            return;
        }
        if (update.state !== 'idle') return;

        hideMessage();
        setUpdateView('download', 'İndirme başlatılıyor…', 0);
        let result = '';
        try {
            result = await InstallUpdate();
        } catch (err) {
            result = '';
        }

        if (result === 'OK') {
            setUpdateView('ready', 'Güncelleme hazır · Yeniden başlat', 1);
        } else if (result === 'NONE') {
            setUpdateView('idle', '', 0);
            updateCard.hidden = true;
            versionBadge.classList.remove('has-update');
            showMessage('Glide zaten en güncel sürümde.', 'notice');
        } else {
            setUpdateView('idle', `Yeni sürüm hazır (${update.version}) · Güncelle`, 0);
            showMessage('Güncelleme indirilemedi. Lütfen daha sonra tekrar deneyin.');
        }
    });

    errorClose.addEventListener('click', hideMessage);

    powerBtn.addEventListener('click', async () => {
        const wantRunning = lastPhase === 'idle' || lastPhase === 'stopping';
        hideMessage();

        acceptState({
            phase: wantRunning ? 'starting' : 'stopping',
            detail: wantRunning ? 'baglanti hazirlaniyor' : 'baglanti kapatiliyor'
        });

        try {
            await (wantRunning ? StartBypass() : StopBypass());
        } catch (err) {
            showMessage(normalizeError(err));
            stateSeq++;
            checkStatus();
        }
    });

    function openSupport() {
        closeSettings();
        supportBtn.classList.remove('has-error');
        supportBtn.classList.add('is-active');
        animateNextSupportRender = true;
        lastSupportHTML = '';
        renderServices();
        openLayer(supportScrim);
        openLayer(supportViewer);

        clearInterval(supportInterval);
        refreshSupportInfo();
        supportInterval = setInterval(refreshSupportInfo, supportPollMs);
    }

    function closeSupport() {
        supportBtn.classList.remove('is-active');
        closeLayer(supportViewer);
        closeLayer(supportScrim);
        clearInterval(supportInterval);
        supportInterval = null;
    }

    supportBtn.addEventListener('click', () => {
        if (isOpen(supportViewer)) {
            closeSupport();
        } else {
            openSupport();
        }
    });

    supportClose.addEventListener('click', closeSupport);
    supportScrim.addEventListener('click', closeSupport);

    function closeSettings() {
        settingsBtn.classList.remove('is-active');
        settingsBtn.setAttribute('aria-expanded', 'false');
        closeLayer(settingsPanel);
    }

    settingsBtn.addEventListener('click', async () => {
        if (isOpen(settingsPanel)) {
            closeSettings();
            return;
        }

        try {
            const [s, autoStart] = await Promise.all([GetSettings(), GetAutoStart()]);
            byId('auto-update-toggle').checked = s.autoUpdate;
            byId('auto-start-bypass-toggle').checked = s.autoStartBypass;
            byId('safe-dns-toggle').checked = s.safeDns;
            byId('network-tuning-toggle').checked = s.networkTuning;
            byId('auto-start-toggle').checked = autoStart;
            byId('isp-select').value = s.ispProfile || 'auto';
        } catch (err) {
            showMessage('Ayarlar okunamadı. Lütfen tekrar deneyin.');
            return;
        }

        closeSupport();
        settingsBtn.classList.add('is-active');
        settingsBtn.setAttribute('aria-expanded', 'true');
        openLayer(settingsPanel);
    });

    settingsClose.addEventListener('click', async () => {
        if (settingsClose.classList.contains('is-busy')) return;
        settingsClose.classList.add('is-busy');

        try {
            const cfg = {
                autoUpdate: byId('auto-update-toggle').checked,
                autoStartBypass: byId('auto-start-bypass-toggle').checked,
                ispProfile: byId('isp-select').value,
                safeDns: byId('safe-dns-toggle').checked,
                networkTuning: byId('network-tuning-toggle').checked,
            };
            const settingsResult = await UpdateSettings(cfg);
            if (settingsResult !== 'OK') {
                showMessage(normalizeError(settingsResult));
                return;
            }

            const autoStartResult = await SetAutoStart(byId('auto-start-toggle').checked);
            if (autoStartResult !== 'OK') {
                showMessage('Başlangıç ayarı kaydedilemedi. Lütfen uygulamayı yönetici olarak çalıştırıp tekrar deneyin.');
                return;
            }

            closeSettings();
            showMessage('Ayarlar kaydedildi.', 'notice');
        } catch (err) {
            showMessage('Ayarlar kaydedilemedi. Lütfen tekrar deneyin.');
        } finally {
            settingsClose.classList.remove('is-busy');
        }
    });

    function openUninstall() {
        closeSettings();
        openLayer(uninstallScrim);
        openLayer(uninstallDialog);
        uninstallCancel.focus();
    }

    function closeUninstall() {
        closeLayer(uninstallDialog);
        closeLayer(uninstallScrim);
    }

    uninstallBtn.addEventListener('click', openUninstall);
    uninstallCancel.addEventListener('click', () => {
        closeUninstall();
        settingsBtn.focus();
    });
    uninstallScrim.addEventListener('click', closeUninstall);
    uninstallConfirm.addEventListener('click', runUninstall);

    async function runUninstall() {
        if (uninstalling) return;
        uninstalling = true;
        hideMessage();
        closeUninstall();
        closeSupport();

        const boot = byId('boot');
        const stepsEl = byId('boot-steps');
        const tickerEl = byId('boot-ticker');
        const subEl = byId('boot-sub');
        const markEl = boot.querySelector('.boot-mark');
        const elapsedEl = byId('boot-elapsed');
        const total = Object.keys(UNINSTALL_STEPS).length;
        const rows = new Map();
        const finished = new Set();
        const started = performance.now();
        let chain = Promise.resolve();

        stepsEl.innerHTML = '';
        subEl.textContent = 'Glide kaldırılıyor';
        tickerEl.textContent = 'kaldırma başlatılıyor';
        elapsedEl.textContent = formatSeconds(0);
        markEl.style.setProperty('--p', '0');
        boot.setAttribute('aria-label', 'Glide kaldırılıyor');
        boot.classList.remove('is-complete');
        boot.classList.add('is-leaving');
        boot.hidden = false;
        void boot.offsetWidth;
        boot.classList.remove('is-leaving');

        const clock = setInterval(() => {
            elapsedEl.textContent = formatSeconds(performance.now() - started);
        }, 40);

        const apply = (event) => {
            const text = UNINSTALL_STEPS[event.step];
            if (!text || finished.has(event.step)) return;
            chain = chain.then(async () => {
                if (finished.has(event.step)) return;
                let row = rows.get(event.step);
                if (!row) {
                    row = createStepRow(text.active, '');
                    stepsEl.appendChild(row);
                    rows.set(event.step, row);
                    tickerEl.textContent = text.ticker;
                    if (event.state === 'active') {
                        await wait(reduceMotion ? 0 : 160);
                        return;
                    }
                    await wait(reduceMotion ? 0 : 120);
                }
                if (event.state === 'active') return;

                const { title, detail } = describeUninstall(event);
                setStepRow(row, event.state, title, detail);
                finished.add(event.step);
                markEl.style.setProperty('--p', String(Math.round(finished.size / total * 100)));
                await wait(reduceMotion ? 0 : 90);
            });
        };

        const stopListening = EventsOn('uninstall_progress', apply);
        let events = null;
        try {
            events = await Uninstall();
        } catch (err) {
            events = null;
        }
        (events || []).forEach(apply);
        await chain;
        stopListening();
        clearInterval(clock);
        elapsedEl.textContent = formatSeconds(performance.now() - started);

        const list = events || [];
        const keptApp = !events || list.some(event => event.step === 'app' && event.state !== 'done');
        const warned = list.some(event => event.state === 'warn');
        subEl.textContent = events ? 'Glide kaldırıldı' : 'Kaldırma tamamlanamadı';
        tickerEl.textContent = keptApp
            ? 'uygulama dosyasını elle silin · kapanıyor'
            : warned ? 'bazı adımlar uyarı verdi · kapanıyor' : 'iz kalmadı · pencere kapanıyor';
        boot.classList.add('is-complete');

        await wait(UNINSTALL_CLOSE_MS);
        FinishUninstall().catch(() => {});
    }

    document.addEventListener('keydown', (e) => {
        if (e.key !== 'Escape' || uninstalling) return;
        if (isOpen(uninstallDialog)) {
            closeUninstall();
        } else if (isOpen(supportViewer)) {
            closeSupport();
        } else if (isOpen(settingsPanel)) {
            closeSettings();
        } else if (isOpen(errorCard)) {
            hideMessage();
        }
    });

    function buildBootPlan(report) {
        const info = (report && report.info) || {};
        const components = info.components || [];
        const plan = [];
        const clampDwell = (ms, base) => Math.min(240, Math.max(base || 110, 90 + (ms || 0) * 3));

        const elevated = !report || report.elevated !== false;
        plan.push({
            title: 'Yönetici yetkisi denetleniyor',
            doneTitle: elevated ? 'Yönetici yetkisi etkin' : 'Yönetici yetkisi gerekli',
            state: elevated ? 'done' : 'failed',
            detail: elevated ? 'tam erişim' : 'ağ sürücüsü için',
            dwell: 110,
            ticker: ['Windows yönetici izni kontrol ediliyor']
        });

        plan.push({
            title: 'Bileşenler doğrulanıyor',
            doneTitle: report && report.startupError
                ? 'Bileşenler yazılamadı'
                : info.filesUpdated ? 'Bileşenler güncellendi' : 'Bileşenler doğrulandı',
            state: report && report.startupError ? 'failed' : 'done',
            detail: components.length
                ? `${components.length} dosya · ${info.filesUpdated ? info.filesUpdated + ' yeni' : 'güncel'}`
                : 'hazır',
            dwell: clampDwell(info.setupMs, 150),
            ticker: components.map(c => `${c.name}  ${formatBytes(c.bytes)}  ${c.updated ? 'yazıldı' : 'bütünlük ✓'}`)
        });

        plan.push({
            title: 'Ağ sürücüsü hazırlanıyor',
            doneTitle: info.driverReady === false ? 'Ağ sürücüsü bulunamadı' : 'Ağ sürücüsü hazır',
            state: info.driverReady === false ? 'failed' : 'done',
            detail: 'WinDivert 64-bit',
            dwell: 120,
            ticker: ['WinDivert64.sys · çekirdek sürücüsü konumu doğrulanıyor']
        });

        plan.push({
            title: 'Kural seti derleniyor',
            doneTitle: 'Kural seti derlendi',
            state: 'done',
            detail: info.domains ? `${info.domains} alan adı · ${info.platforms} platform` : 'hazır',
            dwell: clampDwell(info.rulesMs, 130),
            ticker: [
                `rules.yaml · sürüm ${info.rulesVersion || '-'}`,
                ...(info.platformNames || []).map(name => `platform · ${name}`),
                `list.txt · ${info.hostlistLines || 0} satır yazıldı`
            ]
        });

        plan.push({
            title: 'Ağ bağdaştırıcısı okunuyor',
            doneTitle: info.adapter ? 'Ağ bağdaştırıcısı bulundu' : 'Etkin ağ bulunamadı',
            state: info.adapter ? 'done' : 'skipped',
            detail: info.adapter ? `${info.adapter} · MTU ${info.mtu}` : 'bağlantı bekleniyor',
            dwell: clampDwell(info.networkMs, 110),
            ticker: info.adapter ? [`${info.adapter} · ağ geçidi · MTU ${info.mtu}`] : []
        });

        plan.push({
            title: 'Bağlantı modları yükleniyor',
            doneTitle: info.strategy ? 'Öğrenilmiş mod hazır' : 'Bağlantı modları hazır',
            state: 'done',
            detail: info.strategy
                ? `${strategyName(info.strategy)} · %${Math.round((info.strategyScore || 0) * 100)}`
                : `${info.strategies || 0} mod`,
            dwell: 130,
            ticker: info.strategy
                ? [`adaptive_profiles.json · ${info.strategy}`]
                : ['adaptive_profiles.json · yeni hat, ilk bağlantıda ölçülecek']
        });

        if (report && report.autoConnect) {
            plan.push({
                title: 'Otomatik bağlantı',
                doneTitle: 'Bağlantı başlatılıyor',
                state: 'done',
                detail: 'arka planda',
                dwell: 90,
                ticker: []
            });
        }
        return plan;
    }

    async function hydrateTrace() {
        try {
            const [state, events] = await Promise.all([GetState(), GetConnectTrace()]);
            if (!state || state.phase === 'idle' || !events || !events.length) return;
            lastPhase = PHASE_TEXT[state.phase] ? state.phase : lastPhase;
            events.forEach(event => traceAccept(event, false));
        } catch (err) {
            console.warn('Bağlantı adımları okunamadı:', err);
        }
    }

    async function runBoot() {
        const boot = byId('boot');
        const stepsEl = byId('boot-steps');
        const tickerEl = byId('boot-ticker');
        const subEl = byId('boot-sub');
        const markEl = boot.querySelector('.boot-mark');
        const elapsedEl = byId('boot-elapsed');
        const started = performance.now();
        let skipping = reduceMotion;
        let left = false;

        const leave = async () => {
            if (left) return;
            left = true;
            boot.classList.add('is-leaving');
            document.body.classList.remove('splash');
            hydrateTrace();
            await wait(reduceMotion ? 0 : 460);
            boot.hidden = true;
        };
        const failsafe = setTimeout(leave, BOOT_FAILSAFE_MS);
        boot.addEventListener('click', () => { skipping = true; });

        const clock = setInterval(() => {
            elapsedEl.textContent = formatSeconds(performance.now() - started);
        }, 40);

        let report = null;
        try {
            report = await GetBootReport();
        } catch (err) {
            report = null;
        }
        byId('boot-version').textContent = report && report.version ? report.version : '';

        const plan = buildBootPlan(report);
        const setProgress = (value) => markEl.style.setProperty('--p', String(Math.round(value * 100)));

        for (let i = 0; i < plan.length; i++) {
            const item = plan[i];
            const row = createStepRow(item.title, '');
            stepsEl.appendChild(row);
            setProgress((i + 0.35) / plan.length);

            const tickerGap = item.ticker.length ? Math.max(24, Math.min(46, item.dwell / item.ticker.length)) : 0;
            for (const line of item.ticker) {
                tickerEl.textContent = line;
                if (!skipping) await wait(tickerGap);
            }
            if (!skipping) await wait(Math.max(40, item.dwell - tickerGap * item.ticker.length));

            setStepRow(row, item.state, item.doneTitle, item.detail);
            setProgress((i + 1) / plan.length);
        }

        const failed = plan.some(item => item.state === 'failed');
        tickerEl.textContent = failed ? 'bazı kontroller uyarı verdi' : 'tüm kontroller tamamlandı';
        subEl.textContent = failed ? 'Uyarılarla hazır' : 'Hazır';
        boot.classList.add('is-complete');
        clearInterval(clock);
        elapsedEl.textContent = formatSeconds(performance.now() - started);

        await wait(skipping ? 120 : 420);
        clearTimeout(failsafe);
        await leave();

        if (report && report.startupError) showMessage(report.startupError);
    }

    runBoot();

    setInterval(checkStatus, statusPollMs);
    checkStatus();
});
