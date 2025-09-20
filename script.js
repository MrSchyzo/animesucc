/// <reference path="./lib/hls.js" />

function showSpinner() {
    document.getElementById("loading-spinner").style.display = "block";
}
function hideSpinner() {
    document.getElementById("loading-spinner").style.display = "none";
}
let parser = new DOMParser()
let cors = "https://corsproxy.io/?"

function debouncer(fn, ms) {
    let timer
    return _ => {
        clearTimeout(timer)
        timer = setTimeout(_ => fn.apply(this, arguments), ms)
    }
}

let searchDebouncer = debouncer(search, 333)

function findNodesThat(node, predicate) {
    return [node, ...node.childNodes.values().toArray().flatMap(n => [...findNodesThat(n, predicate)])].filter(predicate)
}

function onloaded(id) {
    document.getElementById(id).removeAttribute("poster")
}

function fullscreen(id) {
    document.getElementById(id).requestFullscreen()
}

async function changeVideoSource(sourceLink, title) {
    let videoMp4 = document.getElementById("video-player-mp4")
    let videoHls = document.getElementById("video-player-hls")
    setVideoLoading("video-player-mp4")
    setVideoLoading("video-player-hls")

    let {src, playlist} = await fetchVideoSource(sourceLink)
    
    document.getElementById("video-container-title").innerText = title
    if (src) {
        videoHls.style.display = "none"
        videoMp4.style.display = "block"
        let videoSource = document.getElementById(`video-player-mp4-source`)
        videoSource.src = src
        videoMp4.load()
    } else if (playlist) {
        videoMp4.style.display = "none"
        videoHls.style.display = "block"
        if (videoHls.canPlayType('application/vnd.apple.mpegurl')) {
            videoHls.src = playlist;
        } else if (Hls.isSupported()) {
            if (window.hls) {
                window.hls.destroy()
            }
            var hls = new Hls();
            window.hls = hls
            hls.loadSource(playlist);
            hls.attachMedia(videoHls);
        }
    }
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

async function search() {
    let query = document.getElementById("query")
    let q = encodeURIComponent(query.value)
    await fetchDOM(`https://www.animesaturn.cx/animelist?search=${q}`)
        .then(doc => findNodesThat(doc.body, n => n.nodeName === "A" && n.classList.contains("badge", "badge-archivio")))
        .then(nodes => nodes.map(n => ({ link: n.href, title: n.innerText })))
        .then(doc => {
            let results = document.getElementById("query-results")
            results.innerHTML = ""
            doc.forEach(d => {
                let div = document.createElement("div")
                let a = document.createElement("a")
                div.onclick = _ => {
                    loadAnime(d.link)
                    results.innerHTML = ""
                }
                a.innerText = d.title
                div.appendChild(a)
                results.appendChild(div)
            })
        })
}

async function loadAnime(link) {
    let episodes = document.getElementById("video-episodes")
    episodes.innerHTML = ""
    document.getElementById("video-container-title").innerHTML = ""
    setVideoLoading("video-player-mp4")
    setVideoLoading("video-player-hls")
    showSpinner();
    await fetchDOM(link)
        .then(doc => findNodesThat(doc.body, n => n.nodeName === "A" && n.classList.contains("bottone-ep")))
        .then(nodes => nodes.map(n => ({ link: n.href, title: n.innerText.trim() })))
        .then(videos => {
            videos.forEach(v => {
                let span = document.createElement("span")
                span.onclick = _ => changeVideoSource(v.link, v.title)
                span.innerText = v.title
                episodes.appendChild(span)
            })
        })
        .finally(hideSpinner);
}

/**
 * Fetch the video source URL from a specific anime episode page.
 * @param {string} url 
 * @returns {Promise<{src: string | null, playlist: string | null}>} The video source URL.
 */
async function fetchVideoSource(url) {
    let videoLink = await getWatchPageURL(url)
    if (!videoLink) throw new Error(`Watch page not found at ${url}`)

    let watchDOM = await fetchDOM(videoLink)

    let sourceUrl = findNodesThat(watchDOM, n => n.nodeName === "SOURCE")[0]?.getAttribute("src") || null
    if (sourceUrl) return {src: sourceUrl, playlist: null}

    let playlist = findNodesThat(watchDOM, n => n.nodeName === "SCRIPT" && n.getAttribute("type") === "text/javascript" && n.textContent.includes(".m3u8"))[0]
        ?.textContent
        ?.match(/(https?:\/\/[^'"]+\.m3u8[^'"]*)/)
        ?.[0]

    return {src: null, playlist: playlist || null}
}

/**
 * Get the URL of the watch page for a specific anime episode.
 * @param {string} url 
 * @returns {Promise<string | null>} The URL of the watch page, if present. Falsey value otherwise.
 */
async function getWatchPageURL(url) {
    let dom = await fetchDOM(url)
    return findNodesThat(dom.body, n => n.nodeName === "A" && n.getAttribute("href")?.includes("watch?"))[0]?.href
}

/**
 * Fetch the DOM of a specific page.
 * @param {string} url 
 * @returns {Promise<Document>} The DOM of the page.
 */
async function fetchDOM(url) {
    return await fetch(`${cors}${url}`)
        .then(response => response.text())
        .then(html => parser.parseFromString(html, "text/html"))
}