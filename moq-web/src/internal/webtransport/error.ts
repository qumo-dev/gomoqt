export type StreamConnErrorCode = number;

export interface StreamConnErrorInfo {
	closeCode: number;
	reason: string;
}

export class StreamConnError extends Error {
	readonly _code: StreamConnErrorCode;

	readonly remote: boolean;

	readonly reason: string;

	get code(): StreamConnErrorCode {
		return this._code;
	}

	constructor(info: StreamConnErrorInfo, remote: boolean = false) {
		super(
			`session was closed for reason "${info.reason}" with code ${info.closeCode} by ${
				remote ? "remote" : "local"
			}`,
		);
		this._code = info.closeCode;
		this.remote = remote;
		this.reason = info.reason;
		Object.setPrototypeOf(this, StreamConnError.prototype);
	}
}

export type WebTransportStreamErrorCode = number;

export interface WebTransportStreamErrorInfo {
	readonly source: "stream";
	readonly streamErrorCode: number;
}

export class WebTransportStreamError extends Error {
	readonly _code: WebTransportStreamErrorCode;

	readonly remote: boolean;

	get code(): WebTransportStreamErrorCode {
		return this._code;
	}

	constructor(init: WebTransportStreamErrorInfo, remote: boolean) {
		super(`stream was reset with code ${init.streamErrorCode}`);
		this._code = init.streamErrorCode;
		this.remote = remote;
		Object.setPrototypeOf(this, WebTransportStreamError.prototype);
	}

	toJSON(): Record<string, unknown> {
		// Detect direct circular reference shortcuts like `error.self = error` to preserve
		// the original behavior that JSON.stringify throws on circular references.
		for (const key of Object.keys(this)) {
			const v = (this as any)[key];
			if (v === this) {
				throw new TypeError("Converting circular structure to JSON");
			}
		}
		return {
			code: this._code,
			message: undefined,
			remote: this.remote,
		};
	}
}
