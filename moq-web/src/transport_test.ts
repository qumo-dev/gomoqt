import { assertEquals } from "@std/assert";
import { isWebKit, selectTransport, transportCandidates } from "./transport.ts";

const userAgents = {
	safariMac:
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.5 Safari/605.1.15",
	safariIPhone:
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.5 Mobile/15E148 Safari/604.1",
	// iPadOS Safari presents itself as a Mac.
	safariIPad:
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.5 Safari/605.1.15",
	chromeIOS:
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/140.0.7339.101 Mobile/15E148 Safari/604.1",
	firefoxIOS:
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) FxiOS/143.0 Mobile/15E148 Safari/605.1.15",
	edgeIOS:
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) EdgiOS/140.0.3485.54 Mobile/15E148 Safari/605.1.15",
	chromeDesktop:
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36",
	chromeAndroid:
		"Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Mobile Safari/537.36",
	edgeDesktop:
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36 Edg/140.0.0.0",
	operaDesktop:
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36 OPR/122.0.0.0",
	firefoxDesktop:
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:143.0) Gecko/20100101 Firefox/143.0",
	deno: "Deno/2.5.0",
} as const;

const webKitCases = [
	{ name: "Safari on macOS", userAgent: userAgents.safariMac, expected: true },
	{ name: "Safari on iPhone", userAgent: userAgents.safariIPhone, expected: true },
	{ name: "Safari on iPad", userAgent: userAgents.safariIPad, expected: true },
	{ name: "Chrome on iOS", userAgent: userAgents.chromeIOS, expected: true },
	{ name: "Firefox on iOS", userAgent: userAgents.firefoxIOS, expected: true },
	{ name: "Edge on iOS", userAgent: userAgents.edgeIOS, expected: true },
	{ name: "Chrome on desktop", userAgent: userAgents.chromeDesktop, expected: false },
	{ name: "Chrome on Android", userAgent: userAgents.chromeAndroid, expected: false },
	{ name: "Edge on desktop", userAgent: userAgents.edgeDesktop, expected: false },
	{ name: "Opera on desktop", userAgent: userAgents.operaDesktop, expected: false },
	{ name: "Firefox on desktop", userAgent: userAgents.firefoxDesktop, expected: false },
	{ name: "Deno", userAgent: userAgents.deno, expected: false },
	{ name: "an empty user agent", userAgent: "", expected: false },
] as const;

for (const c of webKitCases) {
	Deno.test(`isWebKit is ${c.expected} for ${c.name}`, () => {
		const got = isWebKit(c.userAgent);

		assertEquals(got, c.expected);
	});
}

const selectCases = [
	{
		name: "auto picks WebTransport on Chrome",
		kind: "auto",
		env: { userAgent: userAgents.chromeDesktop, hasWebTransport: true },
		expected: "webtransport",
	},
	{
		name: "auto picks WebTransport on Firefox",
		kind: "auto",
		env: { userAgent: userAgents.firefoxDesktop, hasWebTransport: true },
		expected: "webtransport",
	},
	{
		name: "auto picks WebSocket on Safari, though it has WebTransport",
		kind: "auto",
		env: { userAgent: userAgents.safariMac, hasWebTransport: true },
		expected: "websocket",
	},
	{
		name: "auto picks WebSocket on Chrome on iOS, which is WebKit",
		kind: "auto",
		env: { userAgent: userAgents.chromeIOS, hasWebTransport: true },
		expected: "websocket",
	},
	{
		name: "auto picks WebSocket where there is no WebTransport",
		kind: "auto",
		env: { userAgent: userAgents.chromeDesktop, hasWebTransport: false },
		expected: "websocket",
	},
	{
		name: "auto picks WebTransport outside a browser that has one",
		kind: "auto",
		env: { userAgent: undefined, hasWebTransport: true },
		expected: "webtransport",
	},
	{
		name: "webtransport is forced on WebKit",
		kind: "webtransport",
		env: { userAgent: userAgents.safariIPhone, hasWebTransport: true },
		expected: "webtransport",
	},
	{
		name: "webtransport is forced even where there is none",
		kind: "webtransport",
		env: { userAgent: userAgents.chromeDesktop, hasWebTransport: false },
		expected: "webtransport",
	},
	{
		name: "websocket is forced on Chrome",
		kind: "websocket",
		env: { userAgent: userAgents.chromeDesktop, hasWebTransport: true },
		expected: "websocket",
	},
] as const;

for (const c of selectCases) {
	Deno.test(`selectTransport: ${c.name}`, () => {
		const got = selectTransport(c.kind, c.env);

		assertEquals(got, c.expected);
	});
}

const candidateCases = [
	{
		name: "auto tries only WebTransport on Chrome",
		kind: "auto",
		env: { userAgent: userAgents.chromeDesktop, hasWebTransport: true },
		expected: ["webtransport"],
	},
	{
		name: "auto tries WebSocket, then WebTransport, on WebKit",
		kind: "auto",
		env: { userAgent: userAgents.safariIPhone, hasWebTransport: true },
		expected: ["websocket", "webtransport"],
	},
	{
		name: "auto tries only WebSocket where there is no WebTransport",
		kind: "auto",
		env: { userAgent: userAgents.safariIPhone, hasWebTransport: false },
		expected: ["websocket"],
	},
	{
		name: "a forced WebSocket has no second choice",
		kind: "websocket",
		env: { userAgent: userAgents.safariIPhone, hasWebTransport: true },
		expected: ["websocket"],
	},
	{
		name: "a forced WebTransport has no second choice",
		kind: "webtransport",
		env: { userAgent: userAgents.safariIPhone, hasWebTransport: true },
		expected: ["webtransport"],
	},
] as const;

for (const c of candidateCases) {
	Deno.test(`transportCandidates: ${c.name}`, () => {
		const got = transportCandidates(c.kind, c.env);

		assertEquals(got, c.expected);
	});
}
