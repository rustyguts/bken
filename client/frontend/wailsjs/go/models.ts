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
	    bitrate_kbps: number;
	
	    static createFrom(source: any = {}) {
	        return new Metrics(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rtt_ms = source["rtt_ms"];
	        this.packet_loss = source["packet_loss"];
	        this.bitrate_kbps = source["bitrate_kbps"];
	    }
	}

}

