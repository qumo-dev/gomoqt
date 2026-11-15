# @qumo/moq

[![JSR](https://jsr.io/badges/@qumo/moq)](https://jsr.io/@qumo/moq)

TypeScript/JavaScript implementation of Media over QUIC (MOQ Lite) for Deno (and
Node via JSR). It is the web/JS client counterpart to the Go implementation in
this repository.

## Overview

- Targets MOQ Lite over WebTransport
- Works with the Go server (`gomoqt`) for interop testing and browser clients
- Written for Deno first; Node/npm use is supported via JSR shim

## Installation

### Deno

Install from [JSR](https://jsr.io/@qumo/moq):

```bash
deno add jsr:@qumo/moq
```

Then import:

```typescript
import { Session } from "@qumo/moq";
```

### Node.js (npm via JSR)

```bash
npx jsr add @qumo/moq
```

## Usage (minimal)

```ts
import { connect } from "@qumo/moq";

const session = await connect("https://example.com/interop");
const [reader] = await session.subscribe("/broadcast", "video");
```

With several relays, most preferred first, `connectAny` opens the first one that
takes the session: a relay that refuses it (because it is full, say) or does not
answer in time is skipped.

```ts
import { connectAny } from "@qumo/moq";

const { session, url } = await connectAny([
	"https://relay-1.example.com/moq",
	"https://relay-2.example.com/moq",
]);
```

## Development

This project uses Deno as its primary environment (TypeScript-native with built-in fmt/lint/test).

- Prerequisite: [Deno](https://deno.land/) v2.0 or later

Common tasks:

```bash
# Format code
deno task fmt      # or: deno fmt

# Lint code
deno task lint     # or: deno lint

# Run tests
deno task test     # or: deno test --allow-all

# Generate coverage
deno task coverage

# Generate HTML coverage report
deno task coverage:html
```

## Project Structure

```
moq-web/
├── deno.json         # Deno configuration
├── src/              # Source files
│   ├── mod.ts        # Main entry point
│   ├── **/*.ts       # Implementation
│   └── **/*_test.ts  # Tests (Deno convention)
```

## License

MIT
