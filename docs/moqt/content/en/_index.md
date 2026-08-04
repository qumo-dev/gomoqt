---
title: gomoqt
layout: hextra-home
description: Fast, simply designed MoQ implementation for building scalable real-time apps in Go. Familiar API design inspired by net/http.
images:
  - /images/og-image.png
---

{{<hextra/hero-badge>}}
<div class="hx:w-2 hx:h-2 hx:rounded-full hx:bg-primary-400"></div>
	<span>Free, open source</span>
	{{<icon name="arrow-circle-right" attributes="height=14">}}
{{</hextra/hero-badge>}}

<div class="hx:mt-6 hx:mb-6">
{{<hextra/hero-headline>}}
Build live media
&nbsp;<br class="hx:sm:block hx:hidden" />
with MoQ in Go
{{</hextra/hero-headline>}}
</div>

<div class="hx:mb-12">
{{<hextra/hero-subtitle>}}
Fast, simply designed MoQ implementation
&nbsp;<br class="hx:sm:block hx:hidden" />
for building scalable real-time apps.
{{</hextra/hero-subtitle>}}
</div>

<div class="hx:mb-12 hero-btn--green">
{{<hextra/hero-button text="Get Started" link="docs">}}
</div>

{{<hextra/feature-grid>}}
	{{<hextra/feature-card
		title="Familiar API Design"
		subtitle="Inspired by Go's net/http. Idiomatic and intuitive to write."
		class="card--image"
		icon="document-text"
		image="images/familiar-api.png"
		imageClass="card__img"
		style="background: radial-gradient(ellipse at 50% 80%,rgba(16,185,129,0.18),hsla(0,0%,100%,0));"
	>}}
	{{<hextra/feature-card
		title="Pure Go: fast and lightweight"
		subtitle="No cgo or native dependencies; easy cross‑compile. Idiomatic goroutines/channels/context."
		class="card--image"
		icon="sparkles"
		image="https://go.dev/blog/go-brand/Go-Logo/PNG/Go-Logo_Aqua.png"
		imageClass="card__img card__img--go"
		style="background: radial-gradient(ellipse at 50% 80%,rgba(16,185,129,0.18),hsla(0,0%,100%,0));"
	>}}
	{{<hextra/feature-card
		title="MoQ‑Lite Implementation"
		subtitle="Focused on the practical core today, designed to extend tomorrow."
		class="card--image"
		icon="document-text"
		image="images/moq-lite-draft.png"
		imageClass="card__img"
		style="background: radial-gradient(ellipse at 50% 80%,rgba(16,185,129,0.18),hsla(0,0%,100%,0));"
	>}}
	{{<hextra/feature-card
		title="MIT License"
		subtitle="Permissive OSS: use, modify, and redistribute for personal or commercial projects."
		icon="lightning-bolt"
	>}}
	{{<hextra/feature-card
		title="QUIC-agnostic"
		subtitle="Not tied to a specific QUIC library; uses quic-go by default."
		icon="sparkles"
	>}}
	{{<hextra/feature-card
		title="Ready‑to‑run examples"
		subtitle="Tiny server/client samples for instant quickstart—from learning to validation in minutes."
		icon="download"
	>}}
{{</hextra/feature-grid>}}
