package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const (
	fetchURL  = "https://www.escritores.org/concursos/concursos-1/concursos-literarios"
	cachePath = "original.html"
	statePath = ".scrape_state.json"
	userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

type Concurso struct {
	Titulo      string  `json:"titulo"`
	Link        string  `json:"link"`
	Fecha       string  `json:"fecha"`
	FechaISO    string  `json:"fechaISO"`
	Genero      string  `json:"genero"`
	PremioTexto string  `json:"premio"`
	PremioValor float64 `json:"premioValor"`
}

func main() {
	noOpen := flag.Bool("no-open", false, "no abrir el navegador al terminar")
	flag.Parse()

	if err := asegurarOriginal(); err != nil {
		log.Fatalf("Error obteniendo HTML: %v", err)
	}

	file, err := os.Open(cachePath)
	if err != nil {
		log.Fatalf("Error: No se encuentra '%s'.", cachePath)
	}
	defer file.Close()

	doc, err := goquery.NewDocumentFromReader(file)
	if err != nil {
		log.Fatal(err)
	}

	re := regexp.MustCompile(`\(\s*(\d{2}:\d{2}:\d{4})\s*/\s*([^/]+)\s*/\s*([^/]+)`)

	var resultados []Concurso
	doc.Find("#todos li").Each(func(i int, s *goquery.Selection) {
		linkTag := s.Find("a").First()
		titulo := strings.TrimSpace(linkTag.Text())
		href, _ := linkTag.Attr("href")

		match := re.FindStringSubmatch(s.Text())
		if len(match) < 4 {
			return
		}

		fechaStr := match[1]
		generoStr := strings.TrimSpace(match[2])
		premioStr := strings.TrimRight(strings.TrimSpace(match[3]), ")")

		fechaItem, err := time.Parse("02:01:2006", fechaStr)
		if err != nil {
			return
		}

		resultados = append(resultados, Concurso{
			Titulo:      titulo,
			Link:        "https://www.escritores.org" + href,
			Fecha:       fechaItem.Format("02/01/2006"),
			FechaISO:    fechaItem.Format("2006-01-02"),
			Genero:      generoStr,
			PremioTexto: premioStr,
			PremioValor: extraerValorNumerico(premioStr),
		})
	})

	nombreArchivo := "concursos_lista.html"
	crearHTML(nombreArchivo, resultados)

	fmt.Printf("✅ %d concursos procesados.\n", len(resultados))
	if !*noOpen {
		abrirNavegador(nombreArchivo)
	}
}

type estado struct {
	UltimaDescarga string `json:"ultimaDescarga"` // YYYY-MM-DD
}

func leerEstado() estado {
	var e estado
	if b, err := os.ReadFile(statePath); err == nil {
		_ = json.Unmarshal(b, &e)
	}
	return e
}

func guardarEstado(e estado) error {
	b, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(statePath, b, 0o644)
}

func asegurarOriginal() error {
	hoy := time.Now().Format("2006-01-02")
	_, errCache := os.Stat(cachePath)
	hayCache := errCache == nil

	if hayCache && leerEstado().UltimaDescarga == hoy {
		fmt.Println("📦 Ya se descargó hoy — usando original.html")
		return nil
	}

	if err := descargarHTML(fetchURL, cachePath); err != nil {
		if hayCache {
			fmt.Printf("⚠️  Error al descargar (%v). Continuando con caché anterior.\n", err)
			return nil
		}
		return err
	}
	if err := guardarEstado(estado{UltimaDescarga: hoy}); err != nil {
		fmt.Printf("⚠️  No se pudo guardar el estado: %v\n", err)
	}
	return nil
}

func descargarHTML(url, dst string) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "es-ES,es;q=0.9,en;q=0.8")
	if info, err := os.Stat(dst); err == nil {
		req.Header.Set("If-Modified-Since", info.ModTime().UTC().Format(http.TimeFormat))
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		now := time.Now()
		_ = os.Chtimes(dst, now, now)
		fmt.Println("✅ El servidor reporta sin cambios (304)")
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d %s", resp.StatusCode, resp.Status)
	}

	tmp := dst + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return err
	}
	fmt.Println("✅ HTML descargado y guardado")
	return nil
}

func extraerValorNumerico(texto string) float64 {
	re := regexp.MustCompile(`[\d\.,]+`)
	nums := re.FindString(texto)
	if nums == "" {
		return 0.0
	}
	nums = strings.ReplaceAll(nums, ".", "")
	nums = strings.ReplaceAll(nums, ",", ".")
	val, err := strconv.ParseFloat(nums, 64)
	if err != nil {
		return 0.0
	}
	return val
}

func crearHTML(filename string, data []Concurso) {
	f, err := os.Create(filename)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	jsonData, err := json.Marshal(data)
	if err != nil {
		log.Fatal(err)
	}

	html := strings.Replace(htmlTemplate, "__DATA__", string(jsonData), 1)
	html = strings.Replace(html, "__TOTAL__", fmt.Sprintf("%d", len(data)), 1)
	f.WriteString(html)
}

func abrirNavegador(url string) {
	var err error
	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	}
	if err != nil {
		log.Println("Error abriendo navegador:", err)
	}
}

const htmlTemplate = `<!DOCTYPE html>
<html lang="es">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Concursos Literarios</title>
<style>
  :root {
    --primary: #0d5e80;
    --primary-dark: #084560;
    --primary-light: #e8f3f8;
    --accent: #16a34a;
    --accent-soft: #ecfdf5;
    --bg: #f5f7fa;
    --surface: #ffffff;
    --border: #e3e8ee;
    --border-strong: #cdd5df;
    --text: #1a2733;
    --text-muted: #6b7785;
    --text-faint: #98a2b3;
    --shadow-sm: 0 1px 2px rgba(15, 23, 42, 0.05);
    --shadow-md: 0 4px 12px -2px rgba(15, 23, 42, 0.08), 0 2px 4px -1px rgba(15, 23, 42, 0.04);
    --shadow-lg: 0 12px 28px -6px rgba(15, 23, 42, 0.12), 0 4px 10px -2px rgba(15, 23, 42, 0.06);
    --radius: 14px;
    --radius-sm: 8px;
  }
  * { box-sizing: border-box; }
  html, body { margin: 0; padding: 0; }
  body {
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
    background: var(--bg);
    color: var(--text);
    line-height: 1.5;
    -webkit-font-smoothing: antialiased;
  }
  .container { max-width: 1040px; margin: 0 auto; padding: 28px 20px 80px; }

  /* Header */
  .page-head { display: flex; justify-content: space-between; align-items: flex-end; gap: 16px; margin-bottom: 18px; flex-wrap: wrap; }
  h1 { font-size: 1.75rem; color: var(--primary-dark); margin: 0; font-weight: 700; letter-spacing: -0.01em; }
  h1 .emoji { margin-right: 6px; }
  .stats { color: var(--text-muted); font-size: 0.9rem; }
  .stats strong { color: var(--text); font-weight: 600; }

  /* Filter bar */
  .filter-bar {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    box-shadow: var(--shadow-sm);
    margin-bottom: 22px;
    overflow: hidden;
    position: sticky;
    top: 12px;
    z-index: 10;
    backdrop-filter: saturate(180%) blur(8px);
  }
  .filter-bar-top {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 14px 18px;
    gap: 12px;
  }
  .filter-bar-title {
    display: flex; align-items: center; gap: 8px;
    font-weight: 600; color: var(--text); font-size: 0.95rem;
  }
  .filter-bar-title .badge {
    background: var(--primary);
    color: white;
    font-size: 0.72rem;
    padding: 1px 7px;
    border-radius: 999px;
    font-weight: 600;
  }
  .toggle-btn {
    border: 1px solid var(--border-strong);
    background: var(--surface);
    border-radius: var(--radius-sm);
    padding: 6px 12px;
    font-size: 0.85rem;
    color: var(--text);
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    transition: all 0.15s;
  }
  .toggle-btn:hover { background: var(--primary-light); border-color: var(--primary); color: var(--primary); }

  .active-chips {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    padding: 0 18px 14px;
  }
  .chip {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    background: var(--primary-light);
    color: var(--primary-dark);
    border: 1px solid #cce4ee;
    padding: 4px 4px 4px 11px;
    border-radius: 999px;
    font-size: 0.82rem;
    font-weight: 500;
  }
  .chip.persistent { background: #f1f5f9; color: var(--text); border-color: var(--border); }
  .chip .chip-x {
    cursor: pointer;
    opacity: 0.55;
    font-weight: 700;
    width: 18px;
    height: 18px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border-radius: 999px;
    line-height: 1;
    font-size: 0.95rem;
  }
  .chip .chip-x:hover { opacity: 1; background: rgba(0,0,0,0.08); }
  .chip-empty { color: var(--text-faint); font-size: 0.85rem; font-style: italic; padding: 2px 0; }

  /* Editor */
  .editor {
    border-top: 1px solid var(--border);
    background: linear-gradient(180deg, #fafbfc 0%, #ffffff 100%);
    padding: 18px;
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 16px 18px;
  }
  .editor.hidden { display: none; }
  .field { display: flex; flex-direction: column; gap: 6px; }
  .field.full { grid-column: 1 / -1; }
  .field label {
    font-size: 0.72rem;
    color: var(--text-muted);
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.06em;
  }
  .field input[type="text"], .field input[type="date"], .field select {
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    padding: 8px 11px;
    font-size: 0.95rem;
    background: white;
    color: var(--text);
    font-family: inherit;
    transition: all 0.15s;
  }
  .field input:focus, .field select:focus {
    outline: none;
    border-color: var(--primary);
    box-shadow: 0 0 0 3px rgba(13, 94, 128, 0.15);
  }
  .genres-grid { display: flex; flex-wrap: wrap; gap: 6px; }
  .genre-toggle {
    display: inline-flex; align-items: center; gap: 4px;
    background: white;
    border: 1px solid var(--border-strong);
    border-radius: 999px;
    padding: 5px 12px;
    font-size: 0.85rem;
    cursor: pointer;
    user-select: none;
    transition: all 0.15s;
    color: var(--text);
  }
  .genre-toggle:hover { border-color: var(--primary); color: var(--primary); }
  .genre-toggle.active {
    background: var(--primary);
    border-color: var(--primary);
    color: white;
    font-weight: 600;
  }
  .actions { display: flex; gap: 8px; flex-wrap: wrap; }
  .btn {
    border: 1px solid var(--border-strong);
    background: white;
    border-radius: var(--radius-sm);
    padding: 7px 14px;
    font-size: 0.85rem;
    cursor: pointer;
    color: var(--text);
    font-family: inherit;
    font-weight: 500;
    transition: all 0.15s;
  }
  .btn:hover { background: var(--bg); border-color: var(--text-muted); }
  .btn-primary { background: var(--primary); color: white; border-color: var(--primary); }
  .btn-primary:hover { background: var(--primary-dark); border-color: var(--primary-dark); }

  /* List */
  .list { display: flex; flex-direction: column; gap: 10px; }
  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 14px 18px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    text-decoration: none;
    color: inherit;
    transition: all 0.15s;
    box-shadow: var(--shadow-sm);
    gap: 16px;
  }
  .row:hover {
    border-color: var(--primary);
    box-shadow: var(--shadow-md);
    transform: translateY(-1px);
  }
  .main-info { flex: 1; min-width: 0; }
  .title {
    font-weight: 600;
    color: var(--text);
    font-size: 1rem;
    display: block;
    margin-bottom: 7px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .row:hover .title { color: var(--primary-dark); }
  .meta { font-size: 0.8rem; color: var(--text-muted); display: flex; gap: 8px; flex-wrap: wrap; }
  .meta-item {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    background: var(--bg);
    border: 1px solid var(--border);
    padding: 2px 9px;
    border-radius: 999px;
  }
  .meta-item .ic { opacity: 0.7; }
  .prize-info {
    text-align: right;
    min-width: 130px;
    max-width: 220px;
    flex-shrink: 0;
  }
  .prize-amount {
    font-weight: 700;
    color: var(--accent);
    font-size: 1.05rem;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    display: block;
    background: var(--accent-soft);
    padding: 4px 10px;
    border-radius: var(--radius-sm);
  }
  .empty {
    text-align: center;
    padding: 64px 20px;
    color: var(--text-muted);
    background: var(--surface);
    border: 1px dashed var(--border-strong);
    border-radius: var(--radius);
  }
  .empty.hidden { display: none; }

  @media (max-width: 700px) {
    .editor { grid-template-columns: 1fr; }
    .row { flex-direction: column; align-items: stretch; }
    .prize-info { text-align: left; min-width: 0; max-width: none; }
    .prize-amount { display: inline-block; }
    h1 { font-size: 1.4rem; }
  }
</style>
</head>
<body>
<div class="container">
  <header class="page-head">
    <h1><span class="emoji">📝</span>Concursos Literarios</h1>
    <div class="stats">Mostrando <strong id="visible-count">0</strong> de <strong>__TOTAL__</strong> concursos</div>
  </header>

  <section class="filter-bar">
    <div class="filter-bar-top">
      <div class="filter-bar-title">
        <span>🔍 Filtros aplicados</span>
        <span class="badge" id="active-count">0</span>
      </div>
      <button class="toggle-btn" id="toggle-btn">⚙️ <span id="toggle-label">Editar filtros</span></button>
    </div>
    <div class="active-chips" id="active-chips"></div>
    <div class="editor hidden" id="editor">
      <div class="field">
        <label for="f-title">Texto en título</label>
        <input id="f-title" type="text" placeholder="(España)">
      </div>
      <div class="field">
        <label for="f-fecha">Fecha límite mínima</label>
        <input id="f-fecha" type="date">
      </div>
      <div class="field full">
        <label>Géneros (cualquiera coincide)</label>
        <div class="genres-grid" id="genres-grid"></div>
      </div>
      <div class="field">
        <label for="f-sort">Ordenar por</label>
        <select id="f-sort">
          <option value="premio-desc">💰 Premio (mayor primero)</option>
          <option value="premio-asc">💰 Premio (menor primero)</option>
          <option value="fecha-asc">📅 Fecha (próximos primero)</option>
          <option value="fecha-desc">📅 Fecha (lejanos primero)</option>
        </select>
      </div>
      <div class="field">
        <label>&nbsp;</label>
        <div class="actions">
          <button class="btn btn-primary" id="btn-reset">↻ Restablecer</button>
          <button class="btn" id="btn-clear">Limpiar todo</button>
        </div>
      </div>
    </div>
  </section>

  <div class="list" id="list"></div>
  <div class="empty hidden" id="empty">No hay concursos que coincidan con los filtros.</div>
</div>

<script>
const DATA = __DATA__;
const DEFAULTS = {
  title: '(España)',
  fechaDesde: '2026-06-01',
  generos: ['narrativa', 'relato', 'microrrelato', 'cuento', 'novela'],
  sort: 'premio-desc',
};
const EXTRA_GENRES = ['poesía', 'ensayo', 'teatro', 'biografía', 'crónica', 'periodismo', 'infantil', 'juvenil'];
const GENRE_OPTS = [...new Set([...DEFAULTS.generos, ...EXTRA_GENRES])];

let state = { ...DEFAULTS, generos: [...DEFAULTS.generos] };

const $ = id => document.getElementById(id);

function escapeHtml(s) {
  return String(s ?? '').replace(/[&<>"']/g, c => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
  }[c]));
}

function formatDate(iso) {
  if (!iso) return '';
  const [y, m, d] = iso.split('-');
  return d + '/' + m + '/' + y;
}

function sortLabel(s) {
  return ({
    'premio-desc': '💰 Premio ↓',
    'premio-asc': '💰 Premio ↑',
    'fecha-asc': '📅 Fecha ↑',
    'fecha-desc': '📅 Fecha ↓',
  })[s] || s;
}

function applyFilters() {
  return DATA.filter(c => {
    if (state.title) {
      const needle = state.title.toLowerCase();
      if (!c.titulo.toLowerCase().includes(needle)) return false;
    }
    if (state.fechaDesde && c.fechaISO < state.fechaDesde) return false;
    if (state.generos.length > 0) {
      const g = (c.genero || '').toLowerCase();
      if (!state.generos.some(x => g.includes(x.toLowerCase()))) return false;
    }
    return true;
  });
}

function sortItems(items) {
  return items.sort((a, b) => {
    switch (state.sort) {
      case 'premio-desc': return b.premioValor - a.premioValor;
      case 'premio-asc': return a.premioValor - b.premioValor;
      case 'fecha-asc': return a.fechaISO.localeCompare(b.fechaISO);
      case 'fecha-desc': return b.fechaISO.localeCompare(a.fechaISO);
      default: return 0;
    }
  });
}

function render() {
  const filtered = sortItems(applyFilters());
  $('visible-count').textContent = filtered.length;
  const list = $('list');
  list.innerHTML = '';
  if (filtered.length === 0) {
    $('empty').classList.remove('hidden');
  } else {
    $('empty').classList.add('hidden');
    const frag = document.createDocumentFragment();
    for (const c of filtered) {
      const a = document.createElement('a');
      a.href = c.link;
      a.target = '_blank';
      a.rel = 'noopener noreferrer';
      a.className = 'row';
      a.innerHTML =
        '<div class="main-info">' +
          '<span class="title" title="' + escapeHtml(c.titulo) + '">' + escapeHtml(c.titulo) + '</span>' +
          '<div class="meta">' +
            '<span class="meta-item"><span class="ic">📅</span> ' + escapeHtml(c.fecha) + '</span>' +
            '<span class="meta-item"><span class="ic">📖</span> ' + escapeHtml(c.genero) + '</span>' +
          '</div>' +
        '</div>' +
        '<div class="prize-info" title="' + escapeHtml(c.premio) + '">' +
          '<span class="prize-amount">' + escapeHtml(c.premio || '—') + '</span>' +
        '</div>';
      frag.appendChild(a);
    }
    list.appendChild(frag);
  }
  renderChips();
}

function renderChips() {
  const box = $('active-chips');
  box.innerHTML = '';
  const chips = [];
  if (state.title) chips.push({ key: 'title', label: 'Título incluye «' + state.title + '»' });
  if (state.fechaDesde) chips.push({ key: 'fechaDesde', label: 'Fecha ≥ ' + formatDate(state.fechaDesde) });
  if (state.generos.length > 0) chips.push({ key: 'generos', label: 'Géneros: ' + state.generos.join(', ') });

  $('active-count').textContent = chips.length;

  if (chips.length === 0) {
    const e = document.createElement('span');
    e.className = 'chip-empty';
    e.textContent = 'Sin filtros activos · mostrando todos los concursos';
    box.appendChild(e);
  } else {
    for (const chip of chips) {
      const el = document.createElement('span');
      el.className = 'chip';
      el.innerHTML = '<span>' + escapeHtml(chip.label) + '</span>';
      const x = document.createElement('span');
      x.className = 'chip-x';
      x.textContent = '×';
      x.title = 'Quitar filtro';
      x.onclick = (ev) => { ev.preventDefault(); clearFilter(chip.key); };
      el.appendChild(x);
      box.appendChild(el);
    }
  }

  const sortChip = document.createElement('span');
  sortChip.className = 'chip persistent';
  sortChip.textContent = 'Orden: ' + sortLabel(state.sort);
  box.appendChild(sortChip);
}

function clearFilter(key) {
  if (key === 'title') { state.title = ''; $('f-title').value = ''; }
  if (key === 'fechaDesde') { state.fechaDesde = ''; $('f-fecha').value = ''; }
  if (key === 'generos') { state.generos = []; renderGenres(); }
  render();
}

function renderGenres() {
  const g = $('genres-grid');
  g.innerHTML = '';
  for (const opt of GENRE_OPTS) {
    const el = document.createElement('span');
    el.className = 'genre-toggle' + (state.generos.includes(opt) ? ' active' : '');
    el.textContent = opt;
    el.onclick = () => {
      if (state.generos.includes(opt)) {
        state.generos = state.generos.filter(x => x !== opt);
      } else {
        state.generos = [...state.generos, opt];
      }
      renderGenres();
      render();
    };
    g.appendChild(el);
  }
}

function applyDefaults() {
  state = { ...DEFAULTS, generos: [...DEFAULTS.generos] };
  $('f-title').value = state.title;
  $('f-fecha').value = state.fechaDesde;
  $('f-sort').value = state.sort;
  renderGenres();
  render();
}

function clearAll() {
  state = { title: '', fechaDesde: '', generos: [], sort: state.sort };
  $('f-title').value = '';
  $('f-fecha').value = '';
  renderGenres();
  render();
}

function init() {
  $('f-title').value = state.title;
  $('f-fecha').value = state.fechaDesde;
  $('f-sort').value = state.sort;

  $('f-title').addEventListener('input', e => { state.title = e.target.value; render(); });
  $('f-fecha').addEventListener('change', e => { state.fechaDesde = e.target.value; render(); });
  $('f-sort').addEventListener('change', e => { state.sort = e.target.value; render(); });
  $('btn-reset').addEventListener('click', applyDefaults);
  $('btn-clear').addEventListener('click', clearAll);
  $('toggle-btn').addEventListener('click', () => {
    const ed = $('editor');
    const open = ed.classList.toggle('hidden');
    $('toggle-label').textContent = open ? 'Editar filtros' : 'Ocultar editor';
  });

  renderGenres();
  render();
}

init();
</script>
</body>
</html>
`
