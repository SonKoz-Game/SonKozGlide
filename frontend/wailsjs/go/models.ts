export namespace engine {
	
	export class BootFile {
	    name: string;
	    bytes: number;
	    updated: boolean;
	
	    static createFrom(source: any = {}) {
	        return new BootFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.bytes = source["bytes"];
	        this.updated = source["updated"];
	    }
	}
	export class BootInfo {
	    components: BootFile[];
	    platformNames: string[];
	    files: number;
	    filesUpdated: number;
	    setupMs: number;
	    driverReady: boolean;
	    rulesVersion: string;
	    domains: number;
	    platforms: number;
	    hostlistLines: number;
	    rulesMs: number;
	    adapter: string;
	    mtu: number;
	    adapters: number;
	    networkMs: number;
	    strategy: string;
	    strategyScore: number;
	    strategies: number;
	
	    static createFrom(source: any = {}) {
	        return new BootInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.components = this.convertValues(source["components"], BootFile);
	        this.platformNames = source["platformNames"];
	        this.files = source["files"];
	        this.filesUpdated = source["filesUpdated"];
	        this.setupMs = source["setupMs"];
	        this.driverReady = source["driverReady"];
	        this.rulesVersion = source["rulesVersion"];
	        this.domains = source["domains"];
	        this.platforms = source["platforms"];
	        this.hostlistLines = source["hostlistLines"];
	        this.rulesMs = source["rulesMs"];
	        this.adapter = source["adapter"];
	        this.mtu = source["mtu"];
	        this.adapters = source["adapters"];
	        this.networkMs = source["networkMs"];
	        this.strategy = source["strategy"];
	        this.strategyScore = source["strategyScore"];
	        this.strategies = source["strategies"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ProgressEvent {
	    run: number;
	    seq: number;
	    step: string;
	    state: string;
	    detail?: string;
	    index?: number;
	    total?: number;
	    ok?: number;
	    count?: number;
	    latency?: number;
	    at: number;
	
	    static createFrom(source: any = {}) {
	        return new ProgressEvent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.run = source["run"];
	        this.seq = source["seq"];
	        this.step = source["step"];
	        this.state = source["state"];
	        this.detail = source["detail"];
	        this.index = source["index"];
	        this.total = source["total"];
	        this.ok = source["ok"];
	        this.count = source["count"];
	        this.latency = source["latency"];
	        this.at = source["at"];
	    }
	}
	export class ServiceCheck {
	    id: string;
	    ok: number;
	    count: number;
	    latency: number;
	    failed: string[];
	
	    static createFrom(source: any = {}) {
	        return new ServiceCheck(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.ok = source["ok"];
	        this.count = source["count"];
	        this.latency = source["latency"];
	        this.failed = source["failed"];
	    }
	}
	export class Status {
	    phase: string;
	    active: boolean;
	    healthy: boolean;
	    profile: string;
	    detail: string;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.phase = source["phase"];
	        this.active = source["active"];
	        this.healthy = source["healthy"];
	        this.profile = source["profile"];
	        this.detail = source["detail"];
	    }
	}
	export class TuningFinding {
	    name: string;
	    status: string;
	    detail: string;
	
	    static createFrom(source: any = {}) {
	        return new TuningFinding(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.status = source["status"];
	        this.detail = source["detail"];
	    }
	}
	export class TuningReport {
	    // Go type: time
	    ranAt: any;
	    findings: TuningFinding[];
	
	    static createFrom(source: any = {}) {
	        return new TuningReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ranAt = this.convertValues(source["ranAt"], null);
	        this.findings = this.convertValues(source["findings"], TuningFinding);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace main {
	
	export class BootReport {
	    info: engine.BootInfo;
	    version: string;
	    elevated: boolean;
	    autoConnect: boolean;
	    startupError: string;
	
	    static createFrom(source: any = {}) {
	        return new BootReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.info = this.convertValues(source["info"], engine.BootInfo);
	        this.version = source["version"];
	        this.elevated = source["elevated"];
	        this.autoConnect = source["autoConnect"];
	        this.startupError = source["startupError"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class UninstallEvent {
	    step: string;
	    state: string;
	    reason?: string;
	    items?: string[];
	    files?: number;
	    bytes?: number;
	    pending?: number;
	
	    static createFrom(source: any = {}) {
	        return new UninstallEvent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.step = source["step"];
	        this.state = source["state"];
	        this.reason = source["reason"];
	        this.items = source["items"];
	        this.files = source["files"];
	        this.bytes = source["bytes"];
	        this.pending = source["pending"];
	    }
	}

}

export namespace settings {
	
	export class Config {
	    autoUpdate: boolean;
	    autoStartBypass: boolean;
	    ispProfile: string;
	    safeDns: boolean;
	    networkTuning: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.autoUpdate = source["autoUpdate"];
	        this.autoStartBypass = source["autoStartBypass"];
	        this.ispProfile = source["ispProfile"];
	        this.safeDns = source["safeDns"];
	        this.networkTuning = source["networkTuning"];
	    }
	}

}

