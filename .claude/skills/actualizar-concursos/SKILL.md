---
name: actualizar-concursos
description: Regenera el HTML de concursos literarios (descarga de escritores.org una vez por día), lo copia a docs/index.html y, tras confirmar con el usuario, hace commit y push para que se actualice GitHub Pages. Usar cuando el usuario pida actualizar, publicar o desplegar la lista de concursos.
---

# Actualizar y publicar la lista de concursos

Trabajá siempre desde la raíz del repo.

1. **Generar el HTML**: `go run . -no-open`.
   - El programa descarga la web solo si hoy todavía no se descargó (estado en `.scrape_state.json`); si no, reutiliza `original.html`.
   - Si falla la descarga y hay copia previa, sigue con esa: avisale al usuario si lo ves en la salida.
2. **Preparar Pages**: `mkdir -p docs && cp concursos_lista.html docs/index.html`.
3. **Mostrar cambios**: `git status --short` y `git diff --stat docs/ original.html concursos_lista.html`. Si no hay cambios, avisá que no hay nada nuevo para publicar y frená.
4. **Preguntar al usuario** (AskUserQuestion) si quiere commitear y pushear a `origin/main`. No hagas commit ni push sin su confirmación.
5. Si confirma:
   - `git add docs/index.html concursos_lista.html original.html`
   - `git commit -m "Actualizar lista de concursos (YYYY-MM-DD)"` con la fecha de hoy.
   - `git push origin main`
6. Cerrá con la URL de Pages: `https://kvothe838.github.io/concursos-escritura/` (puede tardar uno o dos minutos en reflejar el cambio).

## Notas
- El remoto es `git@gb:Kvothe838/concursos-escritura.git` (alias SSH `gb`, clave de la cuenta personal). La identidad de Git del repo ya es `Kvothe838`.
- Pages debe estar configurado en Settings → Pages: rama `main`, carpeta `/docs`.
- No uses `git push --force`.
