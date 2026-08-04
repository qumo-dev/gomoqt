---
title: TypeScript / JavaScript
weight: 2
---


## Building Web Applications

A significant feature of MoQ is that it is available on web browsers using WebTransport. This allows for real-time media streaming directly in the browser without the need for additional plugins or software.
We provide a JavaScript client library to facilitate this integration.

### Prerequisites

- A runtime with WebTransport support — a modern browser, or [Deno](https://deno.com/)
- A package manager that can install from [JSR](https://jsr.io/): `deno`, `npm`, `pnpm`, `yarn`, or `bun`

{{% steps %}}

### Install module

The client library is published to JSR as [`@qumo/moq`](https://jsr.io/@qumo/moq).

{{< tabs items="Deno, npm, pnpm, yarn, bun" >}}
{{< tab >}}
```bash
deno add jsr:@qumo/moq
```
{{< /tab >}}
{{< tab >}}
```bash
npx jsr add @qumo/moq
```
{{< /tab >}}
{{< tab >}}
```bash
pnpm dlx jsr add @qumo/moq
```
{{< /tab >}}
{{< tab >}}
```bash
yarn dlx jsr add @qumo/moq
```
{{< /tab >}}
{{< tab >}}
```bash
bunx jsr add @qumo/moq
```
{{< /tab >}}
{{< /tabs >}}

### Importing modules

```ts
import { connect } from "@qumo/moq";
import { parseCatalog } from "@qumo/moq/msf";

const session = await connect("https://localhost:4443/moq");
const [reader] = await session.subscribe("/broadcast", "video");
```

| Entrypoint      | Description                                                       |
|:----------------|:------------------------------------------------------------------|
| `@qumo/moq`     | Core MoQ client — sessions, tracks, groups, and frames.           |
| `@qumo/moq/msf` | MSF streaming format — catalogs, deltas, and timelines.           |

{{% /steps %}}

> [!NOTE] Note: Browser compatibility
> If your browser does not support WebTransport, `moqt` does not work.
> Check the [Can I Use](https://caniuse.com/webtransport) for the latest compatibility information.
