/// <reference path="./lib/hls.js" />

let pageSize = 10
let allEpisodes = []
let currentPage = 0

function showSpinner() {
    document.getElementById("loading-spinner").style.display = "block";
}
function hideSpinner() {
    document.getElementById("loading-spinner").style.display = "none";
}

function debouncer(fn, ms) {
    let timer
    return _ => {
        clearTimeout(timer)
        timer = setTimeout(_ => fn.apply(this, arguments), ms)
    }
}

let searchDebouncer = debouncer(search, 333)

function onloaded(id) {
    document.getElementById(id).removeAttribute("poster")
}

function fullscreen(id) {
    document.getElementById(id).requestFullscreen()
}

function setVideoLoading(id) {
    let video = document.getElementById(id)
    video.pause()
    video.currentTime = 0
    if (window.hls) {
        window.hls.destroy()
    }
    video.setAttribute("poster", "./PlanetASuccLoading.png")
}

async function resolveVideoURL(originalURL, proxiedURL) {
    try {
        const r = await fetch(originalURL, {
            method: 'HEAD',
            signal: AbortSignal.timeout(3000)
        })
        if (r.ok || r.status === 206) return originalURL
    } catch (_) {}
    return proxiedURL
}

async function changeVideoSource(episodeURL, title) {
    let videoMp4 = document.getElementById("video-player-mp4")
    let videoHls = document.getElementById("video-player-hls")
    setVideoLoading("video-player-mp4")
    setVideoLoading("video-player-hls")

    document.getElementById("video-container-title").innerText = title

    const {url: proxiedURL, original_url: originalURL, type} = await fetch(`api/video?url=${encodeURIComponent(episodeURL)}`).then(r => {
        if (!r.ok) throw new Error(`video API error: ${r.status}`)
        return r.json()
    })
    const url = await resolveVideoURL(originalURL, proxiedURL)

    if (type === 'mp4') {
        videoHls.style.display = "none"
        videoMp4.style.display = "block"
        document.getElementById("video-player-mp4-source").src = url
        videoMp4.load()
    } else if (type === 'm3u8') {
        videoMp4.style.display = "none"
        videoHls.style.display = "block"
        if (Hls.isSupported()) {
            if (window.hls) window.hls.destroy()
            const hls = new Hls({ enableWorker: true })
            window.hls = hls
            hls.on(Hls.Events.ERROR, (_, data) => {
                const tag = data.fatal ? '[HLS FATAL]' : '[HLS ERROR]'
                console.error(tag, data.type, data.details, data)
            })
            hls.on(Hls.Events.MANIFEST_PARSED, (_, data) => {
                const rect = videoHls.getBoundingClientRect()
                const targetH = rect.height * (window.devicePixelRatio || 1)
                const levels = data.levels
                let pick = 0
                for (let i = 0; i < levels.length; i++) {
                    if (levels[i].height && levels[i].height <= targetH) pick = i
                }
                hls.currentLevel = pick
            })
            hls.loadSource(url)
            hls.attachMedia(videoHls)
        } else {
            videoHls.src = url
        }
    }
}

async function search() {
    const q = document.getElementById("query").value.trim()
    if (!q) return

    const results = await fetch(`api/search?q=${encodeURIComponent(q)}`).then(r => {
        if (!r.ok) throw new Error(`search API error: ${r.status}`)
        return r.json()
    })

    const container = document.getElementById("query-results")
    container.innerHTML = ""
    results.forEach(r => {
        let div = document.createElement("div")
        let a = document.createElement("a")
        div.onclick = _ => {
            loadAnime(r.link)
            container.innerHTML = ""
        }
        a.innerText = r.name
        div.appendChild(a)
        container.appendChild(div)
    })
}

// Fills a probe row with copies of the widest label, counts how many fit.
async function probePageSize(episodesEl) {
    if (!allEpisodes.length) return 10
    episodesEl.innerHTML = ""

    const fontProbe = document.createElement("span")
    fontProbe.innerText = "Ep 0"
    fontProbe.style.visibility = "hidden"
    episodesEl.appendChild(fontProbe)
    await new Promise(r => requestAnimationFrame(r))
    const font = getComputedStyle(fontProbe).font
    episodesEl.innerHTML = ""

    const ctx = document.createElement("canvas").getContext("2d")
    ctx.font = font
    const widestLabel = allEpisodes.reduce((best, ep) => {
        const label = `Ep ${ep.Number}`
        return ctx.measureText(label).width > ctx.measureText(best).width ? label : best
    }, "Ep 0")

    const probeCount = Math.min(60, allEpisodes.length)
    for (let i = 0; i < probeCount; i++) {
        const span = document.createElement("span")
        span.innerText = widestLabel
        episodesEl.appendChild(span)
    }
    await new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r)))
    const spans = [...episodesEl.querySelectorAll("span")]
    if (!spans.length) return 10
    const firstTop = spans[0].getBoundingClientRect().top
    const rowCount = spans.filter(s => Math.abs(s.getBoundingClientRect().top - firstTop) < 2).length
    return Math.max(rowCount, 1)
}

function renderEpisodePage() {
    const episodesEl = document.getElementById("video-episodes")
    episodesEl.innerHTML = ""
    const totalPages = Math.ceil(allEpisodes.length / pageSize)
    const start = currentPage * pageSize
    allEpisodes.slice(start, start + pageSize).forEach(ep => {
        let span = document.createElement("span")
        span.onclick = _ => {
            document.querySelectorAll("#video-episodes span").forEach(s => s.classList.remove("active"))
            span.classList.add("active")
            changeVideoSource(ep.URL, `Ep ${ep.Number}`)
        }
        span.innerText = `Ep ${ep.Number}`
        episodesEl.appendChild(span)
    })
    if (totalPages <= 1) return

    const pag = document.createElement("div")
    pag.className = "pagination"

    function pageBtn(label, page, disabled, active) {
        const btn = document.createElement("button")
        btn.textContent = label
        if (active) btn.classList.add("active")
        if (disabled) { btn.disabled = true }
        else btn.onclick = () => { currentPage = page; renderEpisodePage() }
        return btn
    }

    function ellipsis() {
        const s = document.createElement("span")
        s.textContent = "…"
        s.style.padding = "6px 4px"
        return s
    }

    pag.appendChild(pageBtn("First", 0, currentPage === 0))
    pag.appendChild(pageBtn("‹", currentPage - 1, currentPage === 0))

    const pages = new Set([0, totalPages - 1])
    for (let i = Math.max(0, currentPage - 2); i <= Math.min(totalPages - 1, currentPage + 2); i++) pages.add(i)
    const sorted = [...pages].sort((a, b) => a - b)
    let prev = -1
    for (const p of sorted) {
        if (p - prev > 1) pag.appendChild(ellipsis())
        pag.appendChild(pageBtn(p + 1, p, false, p === currentPage))
        prev = p
    }

    pag.appendChild(pageBtn("›", currentPage + 1, currentPage === totalPages - 1))
    pag.appendChild(pageBtn("Last", totalPages - 1, currentPage === totalPages - 1))
    episodesEl.appendChild(pag)
}

async function loadAnime(link) {
    const episodesEl = document.getElementById("video-episodes")
    episodesEl.innerHTML = ""
    document.getElementById("video-container-title").innerHTML = ""

    if (window.hls) { window.hls.destroy(); window.hls = null }
    const videoMp4 = document.getElementById("video-player-mp4")
    const videoHls = document.getElementById("video-player-hls")
    document.getElementById("video-player-mp4-source").removeAttribute("src")
    videoMp4.load(); videoMp4.style.display = "none"
    videoHls.removeAttribute("src")
    videoHls.load(); videoHls.style.display = "none"

    showSpinner()
    try {
        const episodes = await fetch(`api/episodes?url=${encodeURIComponent(link)}`).then(r => {
            if (!r.ok) throw new Error(`episodes API error: ${r.status}`)
            return r.json()
        })
        allEpisodes = episodes
        currentPage = 0
        pageSize = await probePageSize(episodesEl)
        renderEpisodePage()
    } finally {
        hideSpinner()
    }
}
