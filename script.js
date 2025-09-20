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

async function changeVideoSource(id, sourceLink, title) {
    let video = document.getElementById(id)
    video.setAttribute("poster", "./PlanetASuccLoading.png")
    let videoSource = document.getElementById(`${id}-source`)
    videoSource.src = await fetchVideoSource(sourceLink)
    video.style.display = "block"
    document.getElementById("video-container-title").innerText = title
    video.load()
}

async function search() {
    let query = document.getElementById("query")
    let q = encodeURIComponent(query.value)
    await fetch(`${cors}https://www.animesaturn.cx/animelist?search=${q}`)
        .then(response => response.text())
        .then(html => parser.parseFromString(html, "text/html"))
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
    showSpinner();
    await fetch(`${cors}${link}`)
        .then(response => response.text())
        .then(html => parser.parseFromString(html, "text/html"))
        .then(doc => findNodesThat(doc.body, n => n.nodeName === "A" && n.classList.contains("bottone-ep")))
        .then(nodes => nodes.map(n => ({ link: n.href, title: n.innerText.trim() })))
        .then(videos => {
            videos.forEach(v => {
                let span = document.createElement("span")
                span.onclick = _ => changeVideoSource("video-player", v.link, v.title)
                span.innerText = v.title
                episodes.appendChild(span)
            })
        })
        .finally(hideSpinner);
}

async function fetchVideoSource(link) {
    return await fetch(`${cors}${link}`)
        .then(response => response.text())
        .then(html => parser.parseFromString(html, "text/html"))
        .then(doc => findNodesThat(doc.body, n => n.nodeName === "A" && n.getAttribute("href")?.includes("watch?"))[0])
        .then(nodes => fetch(`${cors}${nodes.href}`))
        .then(response => response.text())
        .then(html => parser.parseFromString(html, "text/html"))
        .then(doc => findNodesThat(doc, n => n.nodeName === "SOURCE")[0])
        .then(nodes => nodes?.getAttribute("src"))
}