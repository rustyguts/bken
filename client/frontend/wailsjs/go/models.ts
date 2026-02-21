export namespace config {
	
	export class ServerEntry {
	    name: string;
	    addr: string;
	
	    static createFrom(source: any = {}) {
	        return new ServerEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.addr = source["addr"];
	    }
	}
	export class Config {
	    theme: string;
	    username: string;
	    input_device_id: number;
	    output_device_id: number;
	    volume: number;
	    noise_enabled: boolean;
	    noise_level: number;
	    agc_enabled: boolean;
	    agc_level: number;
	    aec_enabled: boolean;
	    vad_enabled: boolean;
	    vad_threshold: number;
	    ptt_enabled: boolean;
	    ptt_key: string;
	    noise_gate_enabled: boolean;
	    noise_gate_threshold: number;
	    notification_volume: number;
	    servers: ServerEntry[];
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.theme = source["theme"];
	        this.username = source["username"];
	        this.input_device_id = source["input_device_id"];
	        this.output_device_id = source["output_device_id"];
	        this.volume = source["volume"];
	        this.noise_enabled = source["noise_enabled"];
	        this.noise_level = source["noise_level"];
	        this.agc_enabled = source["agc_enabled"];
	        this.agc_level = source["agc_level"];
	        this.aec_enabled = source["aec_enabled"];
	        this.vad_enabled = source["vad_enabled"];
	        this.vad_threshold = source["vad_threshold"];
	        this.ptt_enabled = source["ptt_enabled"];
	        this.ptt_key = source["ptt_key"];
	        this.noise_gate_enabled = source["noise_gate_enabled"];
	        this.noise_gate_threshold = source["noise_gate_threshold"];
	        this.notification_volume = source["notification_volume"];
	        this.servers = this.convertValues(source["servers"], ServerEntry);
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
	
	export class AudioDevice {
	    id: number;
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new AudioDevice(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	    }
	}
	export class AutoLogin {
	    username: string;
	    addr: string;
	
	    static createFrom(source: any = {}) {
	        return new AutoLogin(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.username = source["username"];
	        this.addr = source["addr"];
	    }
	}
	export class Metrics {
	    rtt_ms: number;
	    packet_loss: number;
	    jitter_ms: number;
	    bitrate_kbps: number;
	    opus_target_kbps: number;
	    quality_level: string;
	    capture_dropped: number;
	    playback_dropped: number;
	
	    static createFrom(source: any = {}) {
	        return new Metrics(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rtt_ms = source["rtt_ms"];
	        this.packet_loss = source["packet_loss"];
	        this.jitter_ms = source["jitter_ms"];
	        this.bitrate_kbps = source["bitrate_kbps"];
	        this.opus_target_kbps = source["opus_target_kbps"];
	        this.quality_level = source["quality_level"];
	        this.capture_dropped = source["capture_dropped"];
	        this.playback_dropped = source["playback_dropped"];
	    }
	}

}

