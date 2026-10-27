<script setup lang="ts">
import { onMounted, onUnmounted, shallowRef, useId } from 'vue'

withDefaults(defineProps<{ compact?: boolean }>(), { compact: false })
const id = useId().replace(/:/g, '')
const element = shallowRef<HTMLElement | null>(null)
const active = shallowRef(false)
// Outline geometry keeps the brand sculpture independent of fonts and device rendering.
const monogramM = 'M112 366V140Q112 121 131 121H157Q168 121 175 133L253 260L331 133Q338 121 349 121H375Q394 121 394 140V366Q394 385 375 385H348Q329 385 329 366V243L272 334Q265 345 253 345Q241 345 234 334L177 243V366Q177 385 158 385H131Q112 385 112 366Z'
const monogramI = 'M448 140Q448 121 467 121H494Q513 121 513 140V366Q513 385 494 385H467Q448 385 448 366Z'
let visible = true
let intersecting = true
let reduced = false
let observer: IntersectionObserver | undefined
let media: MediaQueryList | undefined

function update() { active.value = visible && intersecting && !reduced }
function onVisibility() { visible = !document.hidden; update() }
function onMotion(event: MediaQueryListEvent) { reduced = event.matches; update() }

onMounted(() => {
  visible = !document.hidden
  media = window.matchMedia('(prefers-reduced-motion: reduce), (max-width: 767px)')
  reduced = media.matches
  media.addEventListener?.('change', onMotion)
  document.addEventListener('visibilitychange', onVisibility)
  if ('IntersectionObserver' in window && element.value) {
    observer = new IntersectionObserver(([entry]) => {
      intersecting = entry?.isIntersecting ?? false
      update()
    })
    observer.observe(element.value)
  }
  update()
})

onUnmounted(() => {
  observer?.disconnect()
  media?.removeEventListener?.('change', onMotion)
  document.removeEventListener('visibilitychange', onVisibility)
})
</script>

<template>
  <div ref="element" class="spectrum-art" :class="{ 'is-active': active, 'is-compact': compact }" aria-hidden="true">
    <div class="spectrum-halo" />
    <svg class="spectrum-ribbon" viewBox="0 0 640 520" fill="none" focusable="false" xmlns="http://www.w3.org/2000/svg">
      <defs>
        <path :id="`${id}-m`" :d="monogramM" />
        <path :id="`${id}-i`" :d="monogramI" />
        <clipPath :id="`${id}-faces`">
          <use :href="`#${id}-m`" /><use :href="`#${id}-i`" />
        </clipPath>
        <linearGradient :id="`${id}-glass`" x1="143" y1="119" x2="428" y2="416" gradientUnits="userSpaceOnUse">
          <stop stop-color="#D5C8FF" />
          <stop offset=".29" stop-color="#B19BEC" />
          <stop offset=".51" stop-color="#E8E4FF" />
          <stop offset=".77" stop-color="#9BDDE7" />
          <stop offset="1" stop-color="#8775E2" />
        </linearGradient>
        <linearGradient :id="`${id}-column`" x1="445" y1="125" x2="534" y2="389" gradientUnits="userSpaceOnUse">
          <stop stop-color="#DDEEFF" />
          <stop offset=".37" stop-color="#B5D9F2" />
          <stop offset=".73" stop-color="#B5ABF1" />
          <stop offset="1" stop-color="#8A72DE" />
        </linearGradient>
        <linearGradient :id="`${id}-depth`" x1="167" y1="140" x2="425" y2="425" gradientUnits="userSpaceOnUse">
          <stop stop-color="#A58CE3" />
          <stop offset=".43" stop-color="#B4A4EB" />
          <stop offset=".75" stop-color="#72BECF" />
          <stop offset="1" stop-color="#6750C6" />
        </linearGradient>
        <linearGradient :id="`${id}-edge`" x1="171" y1="122" x2="434" y2="390" gradientUnits="userSpaceOnUse">
          <stop stop-color="white" stop-opacity=".95" />
          <stop offset=".48" stop-color="#F5F2FF" stop-opacity=".5" />
          <stop offset="1" stop-color="#C5F5F5" stop-opacity=".85" />
        </linearGradient>
        <linearGradient :id="`${id}-sheen`" x1="155" y1="128" x2="439" y2="352" gradientUnits="userSpaceOnUse">
          <stop offset=".1" stop-color="white" stop-opacity="0" />
          <stop offset=".38" stop-color="white" stop-opacity=".46" />
          <stop offset=".54" stop-color="white" stop-opacity=".05" />
          <stop offset=".8" stop-color="white" stop-opacity=".18" />
          <stop offset="1" stop-color="white" stop-opacity="0" />
        </linearGradient>
        <radialGradient :id="`${id}-ground`">
          <stop stop-color="#8575CF" stop-opacity=".19" />
          <stop offset="1" stop-color="#8575CF" stop-opacity="0" />
        </radialGradient>
      </defs>
      <ellipse cx="329" cy="437" rx="226" ry="32" :fill="`url(#${id}-ground)`" />
      <g class="spectrum-orbit" stroke="#AAA5CF" stroke-width=".7" opacity=".4">
        <ellipse cx="320" cy="283" rx="270" ry="129" transform="rotate(-24 320 283)" />
        <ellipse cx="320" cy="283" rx="277" ry="136" transform="rotate(-24 320 283)" stroke-dasharray="250 470" />
      </g>
      <g class="spectrum-fold">
        <g transform="translate(-2 -8) rotate(-8 320 260)">
          <!-- Offset silhouettes form the prism depth; front faces remain an unmistakable MI. -->
          <g transform="translate(18 22)" :fill="`url(#${id}-depth)`">
            <use :href="`#${id}-m`" /><use :href="`#${id}-i`" />
          </g>
          <path d="M394 140L412 162V388L394 366ZM513 140L531 162V388L513 366Z" fill="#8470CF" opacity=".72" />
          <path d="M177 243L195 265V388L177 366ZM272 334L290 356L347 265L329 243Z" fill="#6D92C5" opacity=".5" />
          <use :href="`#${id}-m`" :fill="`url(#${id}-glass)`" :stroke="`url(#${id}-edge)`" stroke-width="1.5" />
          <use :href="`#${id}-i`" :fill="`url(#${id}-column)`" :stroke="`url(#${id}-edge)`" stroke-width="1.5" />
          <g :clip-path="`url(#${id}-faces)`">
            <path d="M90 112H535V400H90Z" :fill="`url(#${id}-sheen)`" />
            <path d="M113 116L280 392H304L137 116Z" fill="white" opacity=".16" />
            <path d="M340 116L173 392H184L351 116Z" fill="white" opacity=".22" />
            <path d="M457 117V387M499 117V387" stroke="white" stroke-opacity=".28" />
          </g>
          <!-- Inset edge reflections describe the bevel without blurring the silhouette. -->
          <path d="M123 355V143Q123 132 135 132H154M342 143Q346 133 355 133H373Q382 133 382 144V354M460 353V144Q460 133 471 133H491" stroke="white" stroke-width="1.6" stroke-linecap="round" opacity=".8" />
          <path d="M187 147L253 255L319 147M122 372Q124 376 132 376H157M339 373H375M459 373H494" stroke="#E7FFFF" stroke-width="1" stroke-linecap="round" opacity=".65" />
        </g>
      </g>
      <circle cx="77" cy="337" r="4" fill="#39C6D6" />
      <circle cx="551" cy="208" r="3.5" fill="#A18BEE" />
      <path d="M519 392h14m-7-7v14" stroke="#9B94BA" stroke-width="1" />
    </svg>
  </div>
</template>

<style scoped>
.spectrum-art { position: relative; width: 100%; aspect-ratio: 1.2; pointer-events: none; isolation: isolate; }
.spectrum-halo { position: absolute; inset: 9% 2%; border-radius: 50%; background: radial-gradient(ellipse at 66% 34%, rgb(185 173 245 / .32), transparent 60%), radial-gradient(ellipse at 32% 70%, rgb(104 215 226 / .24), transparent 55%); }
.spectrum-ribbon { position: relative; width: 100%; height: 100%; overflow: visible; }
.spectrum-fold { transform-origin: center; animation: spectrum-float 12s ease-in-out infinite; animation-play-state: paused; }
.is-active .spectrum-fold { animation-play-state: running; }
.is-compact { aspect-ratio: 1.8; }
.is-compact .spectrum-ribbon { position: absolute; top: -35%; height: 170%; }
@keyframes spectrum-float { 0%, 100% { transform: translateY(0) rotate(-1deg); } 50% { transform: translateY(-10px) rotate(1deg); } }
@media (prefers-reduced-motion: reduce), (max-width: 767px) { .spectrum-fold { animation: none; } }
</style>
