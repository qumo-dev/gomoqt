import { ReceiveStream } from "./receive_stream.ts";
import { SendStream } from "./send_stream.ts";

export interface Stream {
	readonly writable: SendStream;
	readonly readable: ReceiveStream;
}
export interface StreamInit {
	stream: WebTransportBidirectionalStream;
}

class StreamClass {
	readonly writable: SendStream;
	readonly readable: ReceiveStream;
	constructor(init: StreamInit) {
		this.writable = new SendStream({
			stream: init.stream.writable,
		});
		this.readable = new ReceiveStream({
			stream: init.stream.readable,
		});
	}
}

export const Stream: {
	new (init: StreamInit): Stream;
} = StreamClass;
