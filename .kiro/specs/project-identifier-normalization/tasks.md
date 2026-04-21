# Plan de Implementación: Project Identifier Normalization

## Overview

Implementar la normalización de identificadores de proyecto en dos capas: funciones puras en el Store (`NormalizeRemoteURL` y `normalizeProject`), integración en escritura y consulta, actualización de descripciones MCP, y documentación del Memory Protocol.

El orden de implementación es: funciones puras → tests de unidad y propiedad → integración en escritura → integración en consulta → MCP → documentación.

## Tasks

- [x] 1. Añadir `gopter` al módulo Go
  - Ejecutar `go get github.com/leanovate/gopter` y `go get github.com/leanovate/gopter/gen` y `go get github.com/leanovate/gopter/prop`
  - Verificar que `go.mod` y `go.sum` quedan actualizados
  - _Requirements: 11_

- [x] 2. Implementar `NormalizeRemoteURL` en `internal/store/store.go`
  - Añadir la función exportada `NormalizeRemoteURL(rawURL string) string` con el algoritmo del diseño:
    - Trim espacios
    - SSH (`git@host:owner/repo.git`) → regex `git@([^:]+):(.+)`, eliminar `.git`, retornar `host/path`
    - HTTP/HTTPS (`://`) → `url.Parse`, limpiar credenciales, eliminar `.git` del path, retornar `host + path`
    - Cualquier otro formato → retornar `""`
  - Añadir import `"net/url"` si no existe
  - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 2.7, 2.8_

- [x] 3. Implementar `normalizeProject` en `internal/store/store.go`
  - Añadir la función interna `normalizeProject(v string) string` con las reglas del diseño en orden de prioridad:
    1. Vacío o solo espacios → `"unknown"`
    2. Contiene `"://"` o empieza con `"git@"` → `NormalizeRemoteURL` (URL cruda)
    3. Tiene formato `host/owner/repo` (no empieza con `/` ni `X:\`, no contiene `\`, ≥2 segmentos con `/`) → preservar sin modificación
    4. Contiene `/` o `\` → `filepath.Base()` (ruta absoluta del OS)
    5. Caso contrario → valor sin modificación
  - Añadir import `"path/filepath"` si no existe
  - _Requirements: 7.1, 7.2, 7.6, 7.7, 3.1, 3.2, 3.3, 3.4_

- [x] 4. Tests de unidad para `NormalizeRemoteURL` y `normalizeProject`
  - [x] 4.1 Crear `internal/store/normalize_test.go` con tabla de casos de ejemplo
    - Cubrir todos los casos de la tabla del diseño para `NormalizeRemoteURL`: HTTPS con `.git`, SSH, HTTPS sin `.git`, credenciales embebidas, hosts distintos (`gitlab.com`, `bitbucket.org`), string vacío
    - Cubrir todos los casos de la tabla del diseño para `normalizeProject`: ya normalizado, ruta Unix, ruta Windows, nombre simple, vacío, solo espacios
    - _Requirements: 11.1, 11.2, 11.3, 11.4, 11.5, 11.7, 11.8, 11.9, 11.10, 11.11_

  - [ ]* 4.2 Escribir property test — Property 1: Convergencia HTTPS/SSH
    - **Property 1: Convergencia de URLs HTTPS y SSH**
    - Generador: triples `(host, owner, repo)` con strings alfanuméricos válidos
    - Construir `https://host/owner/repo.git` y `git@host:owner/repo.git`
    - Verificar: `NormalizeRemoteURL(https) == NormalizeRemoteURL(ssh) == "host/owner/repo"`
    - **Validates: Requirements 2.1, 2.2, 2.9**

  - [ ]* 4.3 Escribir property test — Property 2: Eliminación de credenciales embebidas
    - **Property 2: Eliminación de credenciales embebidas**
    - Generador: triples `(user, token, "host/owner/repo")` con strings alfanuméricos
    - Construir `https://user:token@host/owner/repo.git`
    - Verificar: el resultado no contiene `user` ni `token`
    - **Validates: Requirements 2.8**

  - [ ]* 4.4 Escribir property test — Property 3: Correctness de `normalizeProject`
    - **Property 3: Correctness de `normalizeProject` para los tres casos de entrada**
    - Sub-caso A: generador de paths Unix (`/a/b/name`) y Windows (`C:\a\b\name`) → verificar `normalizeProject(path) == filepath.Base(path)`
    - Sub-caso B: generador de triples `host/owner/repo` sin `/` inicial ni `\` → verificar preservación sin modificación
    - Sub-caso C: generador de strings simples sin `/` ni `\` → verificar sin modificación
    - **Validates: Requirements 7.1, 7.2, 7.7, 3.1, 3.2, 3.3**

- [x] 5. Checkpoint — Verificar que todos los tests de funciones puras pasan
  - Ejecutar `go test ./internal/store/... -v -run TestNormalize` y `go test ./internal/store/... -v -run TestProperty`
  - Asegurarse de que todos los tests pasan; preguntar al usuario si surgen dudas.

- [x] 6. Integrar `normalizeProject` en escritura del Store
  - [x] 6.1 Actualizar `createSessionTx` en `internal/store/store.go`
    - Aplicar `normalizeProject(params.Project)` antes del INSERT en sessions
    - Preservar `params.Directory` sin modificación (auditoría)
    - _Requirements: 7.3, 4.4_

  - [x] 6.2 Actualizar `AddObservation` en `internal/store/store.go`
    - Aplicar `normalizeProject(p.Project)` al inicio de la función, antes de cualquier comparación de deduplicación o topic
    - Usar el valor normalizado en el INSERT y en las queries de deduplicación/topic
    - _Requirements: 7.3, 4.4_

  - [x] 6.3 Actualizar `AddPrompt` en `internal/store/store.go`
    - Aplicar `normalizeProject(p.Project)` antes del INSERT en user_prompts
    - _Requirements: 7.3, 4.4_

- [x] 7. Integrar normalización en consultas del Store
  - [x] 7.1 Actualizar `RecentSessions` en `internal/store/store.go`
    - Aplicar `normalizeProject(project)` al parámetro de consulta en Go
    - Actualizar el filtro SQL para comparar también con el basename del valor almacenado (compatibilidad histórica):
      ```sql
      AND (s.project = ? OR REVERSE(SPLIT_PART(REVERSE(s.project), '/', 1)) = ? OR REVERSE(SPLIT_PART(REVERSE(REPLACE(s.project, '\', '/')), '/', 1)) = ?)
      ```
    - _Requirements: 7.5, 8.1, 8.2, 8.3, 8.5_

  - [x] 7.2 Actualizar `RecentObservations` en `internal/store/store.go`
    - Misma estrategia que 7.1 para el filtro de `o.project`
    - _Requirements: 7.5, 8.1, 8.2, 8.3, 8.5_

  - [x] 7.3 Actualizar `Search` en `internal/store/store.go`
    - Aplicar `normalizeProject` al parámetro y usar filtro SQL con basename para compatibilidad histórica
    - _Requirements: 7.5, 8.1, 8.2, 8.5_

  - [x] 7.4 Actualizar `RecentPrompts` y `SearchPrompts` en `internal/store/store.go`
    - Aplicar `normalizeProject` al parámetro y usar filtro SQL con basename para compatibilidad histórica
    - _Requirements: 7.5, 8.1, 8.2, 8.5_

  - [x] 7.5 Actualizar `FormatContext` (o función equivalente de contexto) en `internal/store/store.go`
    - Aplicar `normalizeProject` al parámetro de consulta
    - _Requirements: 7.5_

- [x] 8. Tests de integración para normalización en escritura y consulta
  - [x] 8.1 Añadir casos de prueba en `internal/store/store_test.go`
    - Test: insertar con ruta Unix absoluta → verificar que `project` almacenado es el basename
    - Test: insertar con ruta Windows absoluta → verificar que `project` almacenado es el basename
    - Test: insertar con valor ya normalizado `github.com/owner/repo` → verificar que se preserva sin modificación
    - Test: insertar con string vacío → verificar que se almacena `"unknown"`
    - Test: insertar con `project = "github.com/owner/repo"` y consultar con `project = "repo"` → verificar que no retorna (la normalización no debe romper identificadores ya normalizados)
    - _Requirements: 11.7, 11.8, 11.9, 11.10, 11.11_

  - [ ]* 8.2 Escribir property test de integración — Property 4: Compatibilidad con datos históricos
    - **Property 4: Compatibilidad con datos históricos en consultas**
    - Insertar observación con `project = "/home/user/projects/repo"` (ruta absoluta histórica)
    - Consultar con `project = "repo"` (nombre normalizado)
    - Verificar: la observación aparece en los resultados de `RecentObservations` y `Search`
    - **Validates: Requirements 8.1, 8.2, 8.3, 8.5**

  - [ ]* 8.3 Escribir tests de unidad para los casos de normalización en consulta
    - Test: `normalizeProject` aplicado al parámetro de consulta produce el valor correcto para cada tipo de input
    - _Requirements: 11.12_

- [x] 9. Checkpoint — Verificar que todos los tests de integración pasan
  - Ejecutar `go test ./internal/store/... -v -cover`
  - Asegurarse de que todos los tests pasan; preguntar al usuario si surgen dudas.

- [x] 10. Actualizar descripciones de parámetro `project` en `internal/mcp/mcp.go`
  - [x] 10.1 Actualizar descripción de `project` en `mem_save`
    - Indicar: usar remote origin normalizado (`github.com/owner/repo`) o basename del directorio como fallback
    - Incluir ejemplo: `"github.com/vertigo7x/postgram"` para proyectos con git, `"mi-app"` para proyectos sin git
    - _Requirements: 6.1, 6.2, 6.4_

  - [x] 10.2 Actualizar descripción de `project` y `directory` en `mem_session_start`
    - `project`: remote origin normalizado o basename; incluir instrucción de ejecutar `git remote get-url origin` y normalizar
    - `directory`: ruta local completa del workspace (para auditoría)
    - Indicar que si se usa basename fallback, el scope recomendado es `personal`
    - _Requirements: 6.1, 6.2, 6.3, 6.4, 6.5_

  - [x] 10.3 Actualizar descripción de `project` en `mem_session_summary`
    - Misma instrucción que `mem_save`: remote origin normalizado o basename
    - _Requirements: 6.1, 6.2, 6.4_

  - [x] 10.4 Actualizar descripción de `project` en `mem_save_prompt`
    - Misma instrucción que `mem_save`
    - _Requirements: 6.1, 6.2, 6.4_

  - [x] 10.5 Actualizar descripción de `project` en `mem_context`
    - Indicar que el filtro acepta el remote origin normalizado o el basename
    - _Requirements: 6.1, 6.2, 6.4_

- [x] 11. Actualizar documentación del Memory Protocol
  - [x] 11.1 Añadir sección "Project Identifier" en `skills/memory-protocol/SKILL.md`
    - Lógica de detección en orden de prioridad: (1) `git remote get-url origin` + normalización, (2) basename del path con scope `personal`
    - Tabla de transformaciones: HTTPS con `.git`, SSH, sin git
    - Indicar que cuando se usa basename fallback, el scope por defecto es `personal`
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.5, 5.6_

  - [x] 11.2 Añadir sección "Project Identifier Normalization" en `DOCS.md` (sección Features)
    - Explicar lógica de prioridad, tabla de transformaciones, ejemplos de los tres casos
    - Indicar que la normalización ocurre client-side (instrucciones al agente) y server-side (defensa en profundidad)
    - Mencionar que `directory` en `sessions` preserva la ruta original para auditoría
    - Explicar por qué el basename no es suficiente para proyectos compartidos
    - _Requirements: 9.1, 9.2, 9.3, 9.4, 9.5, 9.6_

  - [x] 11.3 Añadir nota sobre normalización de `project` en `docs/AGENT-SETUP.md`
    - En la sección Memory Protocol: indicar que los agentes deben derivar `project` del remote origin normalizado, con fallback al basename
    - Incluir ejemplo correcto vs incorrecto: `"github.com/org/repo"` vs `"/home/alice/projects/repo"`
    - Indicar que proyectos sin git deben usar scope `personal`
    - _Requirements: 10.1, 10.2, 10.3, 10.4_

- [x] 12. Checkpoint final — Verificar que todos los tests pasan
  - Ejecutar `go test ./... -cover`
  - Verificar que no hay regresiones en `internal/mcp/...`, `internal/server/...`, `cmd/postgram/...`
  - Asegurarse de que todos los tests pasan; preguntar al usuario si surgen dudas.

## Notes

- Las tareas marcadas con `*` son opcionales y pueden omitirse para un MVP más rápido
- Cada tarea referencia los requisitos específicos para trazabilidad
- Los property tests usan `gopter` (`github.com/leanovate/gopter`) — añadir al `go.mod` en la tarea 1
- `NormalizeRemoteURL` es exportada (mayúscula) porque es la referencia canónica del Memory Protocol; `normalizeProject` es interna (minúscula)
- La normalización en consultas usa expresiones SQL con `SPLIT_PART` y `REVERSE` para extraer el basename del valor almacenado sin migración de datos
- El campo `directory` en `sessions` siempre preserva la ruta original sin normalizar (auditoría)
