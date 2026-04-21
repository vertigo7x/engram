# Design Document: Project Identifier Normalization

## Overview

Postgram almacena el identificador de proyecto como texto libre sin normalización. Los agentes pueden pasar rutas absolutas del sistema de archivos (`/home/alice/projects/mi-app`, `C:\Users\bob\projects\mi-app`) en lugar de un identificador lógico consistente, lo que fragmenta las memorias del mismo proyecto entre usuarios y máquinas.

La solución tiene dos capas complementarias:

1. **Client-side (Memory Protocol)**: instrucciones explícitas al agente para detectar el remote origin de git y normalizar la URL antes de llamar a cualquier herramienta MCP.
2. **Server-side (Store)**: función `normalizeProject` aplicada en escritura y en consulta como defensa en profundidad y para compatibilidad con datos históricos.

El identificador normalizado tiene el formato `github.com/owner/repo` para proyectos con git remote, o el basename del directorio de trabajo para proyectos sin git (con scope `personal` automático).

### Decisiones de diseño clave

- **No hay migración de datos**: los valores históricos se preservan en la base de datos; la normalización ocurre en tiempo de consulta en ambos lados de la comparación.
- **El Store no infiere el proyecto desde `directory`**: el agente envía el valor ya normalizado en `project`; el Store solo aplica normalización defensiva.
- **`NormalizeRemoteURL` vive en `internal/store`**: es lógica de negocio pura (sin I/O), reutilizable desde tests y desde el Memory Protocol como referencia canónica.
- **La normalización server-side NO debe romper identificadores ya normalizados**: `github.com/owner/repo` contiene `/` pero no debe ser truncado al último segmento.

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│  Agent (Claude Code / OpenCode / Gemini CLI / Codex / etc.)     │
│                                                                  │
│  1. git remote get-url origin  →  NormalizeRemoteURL()          │
│     (instrucción en Memory Protocol / DOCS.md / AGENT-SETUP.md) │
│  2. Envía project="github.com/owner/repo" en todas las tools    │
└──────────────────────────┬──────────────────────────────────────┘
                           │ MCP over HTTP
                           ▼
┌─────────────────────────────────────────────────────────────────┐
│  internal/mcp/mcp.go  (MCP_Handler)                             │
│  - Pasa project tal como llega al Store (sin transformación)    │
│  - Actualiza descripciones de parámetro `project` en tools      │
└──────────────────────────┬──────────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────────────┐
│  internal/store/store.go  (Store)                               │
│                                                                  │
│  normalizeProject(v string) string                              │
│    ├─ Si contiene "://" o empieza con "git@" → NormalizeRemoteURL│
│    ├─ Si contiene "/" o "\" pero NO tiene formato host/owner/repo│
│    │   → filepath.Base() (basename del path)                    │
│    ├─ Si es vacío/solo espacios → "unknown"                     │
│    └─ Caso contrario → valor sin modificación                   │
│                                                                  │
│  Aplicado en:                                                    │
│    CreateSession, AddObservation, AddPrompt (escritura)         │
│    RecentSessions, RecentObservations, Search,                  │
│    RecentPrompts, SearchPrompts, FormatContext (consulta)        │
└──────────────────────────┬──────────────────────────────────────┘
                           │
                           ▼
                    PostgreSQL
```

---

## Components and Interfaces

### 1. `NormalizeRemoteURL(rawURL string) string` — `internal/store`

Función pura exportada. Transforma una Remote_Origin_URL cruda en un Normalized_Project_Name.

```go
// NormalizeRemoteURL transforma una URL de remote origin de git en un
// identificador de proyecto normalizado del formato "host/owner/repo".
//
// Soporta:
//   - HTTPS:  https://github.com/owner/repo.git  → github.com/owner/repo
//   - HTTP:   http://github.com/owner/repo        → github.com/owner/repo
//   - SSH:    git@github.com:owner/repo.git       → github.com/owner/repo
//   - Credenciales embebidas: https://user:token@github.com/owner/repo.git → github.com/owner/repo
//
// Si la URL no puede ser parseada, retorna "".
func NormalizeRemoteURL(rawURL string) string
```

**Algoritmo**:
1. Trim espacios.
2. Si empieza con `git@` (SSH): extraer host y path con regex `git@([^:]+):(.+)`, eliminar `.git` del path, retornar `host/path`.
3. Si contiene `://` (HTTP/HTTPS): parsear con `url.Parse`, eliminar credenciales (`URL.User = nil`), eliminar `.git` del path, retornar `host + path` (sin scheme).
4. Si no coincide con ningún patrón: retornar `""`.

### 2. `normalizeProject(v string) string` — `internal/store` (unexported)

Función pura interna. Aplica normalización defensiva server-side.

```go
// normalizeProject normaliza un valor de project recibido por el Store.
// Reglas (en orden de prioridad):
//   1. Vacío o solo espacios → "unknown"
//   2. Contiene "://" o empieza con "git@" → NormalizeRemoteURL (URL cruda)
//   3. Tiene formato host/owner/repo (≥2 segmentos sin separador de path OS) → preservar
//   4. Contiene "/" o "\" → filepath.Base() (ruta absoluta del OS)
//   5. Caso contrario → valor sin modificación
func normalizeProject(v string) string
```

**Regla crítica para el caso 3**: un valor como `github.com/owner/repo` contiene `/` pero NO debe ser truncado. La heurística para distinguirlo de una ruta absoluta del OS:
- No empieza con `/` ni con letra de unidad Windows (`C:\`, `D:\`, etc.)
- No contiene `\`
- Tiene al menos dos segmentos separados por `/`

Si cumple estas condiciones, se preserva sin modificación.

### 3. Actualización de `createSessionTx` — `internal/store`

```go
// Antes de insertar, normalizar project:
project := normalizeProject(params.Project)
// directory preserva la ruta original para auditoría
directory := params.Directory
```

### 4. Actualización de `AddObservation` — `internal/store`

```go
project := normalizeProject(p.Project)
// Usar project normalizado en INSERT y en comparaciones de deduplicación/topic
```

### 5. Actualización de `AddPrompt` — `internal/store`

```go
project := normalizeProject(p.Project)
```

### 6. Normalización en consultas — `internal/store`

Para las funciones que filtran por `project` (`RecentSessions`, `RecentObservations`, `Search`, `RecentPrompts`, `SearchPrompts`, `FormatContext`), la comparación debe normalizarse en ambos lados.

**Estrategia**: usar una función SQL auxiliar o aplicar la normalización en Go antes de construir la query.

Dado que PostgreSQL no tiene una función nativa equivalente a `normalizeProject`, la estrategia es:

```sql
-- Para compatibilidad con datos históricos (rutas absolutas):
-- Comparar el valor almacenado normalizado con el valor de consulta normalizado
-- usando CASE WHEN en SQL o aplicando normalizeProject() en Go al parámetro de consulta.
```

**Implementación**: aplicar `normalizeProject(project)` al parámetro de consulta en Go antes de pasarlo a la query SQL. Esto es suficiente para el caso forward (nuevos datos ya normalizados). Para datos históricos con rutas absolutas almacenadas, se necesita normalización en el lado de la base de datos.

**Solución para datos históricos**: usar una expresión SQL que extraiga el basename del valor almacenado:

```sql
-- Comparación normalizada en PostgreSQL:
-- Extraer el último segmento del path almacenado y comparar con el valor normalizado
AND (
    o.project = ?                                    -- match exacto (ya normalizado)
    OR REVERSE(SPLIT_PART(REVERSE(o.project), '/', 1)) = ?   -- basename Unix
    OR REVERSE(SPLIT_PART(REVERSE(REPLACE(o.project, '\', '/')), '/', 1)) = ?  -- basename Windows
)
```

Simplificado con una función helper SQL reutilizable definida como expresión inline.

### 7. Actualizaciones en `internal/mcp/mcp.go`

Actualizar las descripciones del parámetro `project` en las tools: `mem_save`, `mem_session_start`, `mem_session_summary`, `mem_save_prompt`, `mem_context`.

Ejemplo para `mem_session_start`:
```
project: "Normalized project identifier. Use the git remote origin URL normalized
to 'host/owner/repo' format (e.g. 'github.com/vertigo7x/postgram'). If no git
remote exists, use the directory basename (e.g. 'my-app') and set scope to
'personal'. Run: git remote get-url origin, then strip https://, http://, git@,
colon separator, and .git suffix."
```

### 8. Actualizaciones de documentación

- `DOCS.md`: nueva sección "Project Identifier Normalization" en Features, tabla de transformaciones, ejemplos de los tres casos.
- `skills/memory-protocol/SKILL.md`: sección "Project Identifier" con lógica de detección y ejemplos.
- `docs/AGENT-SETUP.md`: nota sobre normalización de `project` en la sección Memory Protocol.

---

## Data Models

No se requieren cambios de esquema. La normalización es transparente en tiempo de ejecución.

### Flujo de datos para un proyecto con git remote

```
Agent input:  project = "https://github.com/vertigo7x/postgram.git"
              directory = "/home/alice/projects/postgram"

Store write:  project = "github.com/vertigo7x/postgram"  ← normalizeProject()
              directory = "/home/alice/projects/postgram"  ← preservado

DB stored:    project = "github.com/vertigo7x/postgram"
              directory = "/home/alice/projects/postgram"
```

### Flujo de datos para datos históricos (compatibilidad)

```
DB stored (histórico):  project = "/home/alice/projects/postgram"

Query input:  project = "github.com/vertigo7x/postgram"
              → normalizeProject() → "github.com/vertigo7x/postgram"

SQL WHERE:    o.project = 'github.com/vertigo7x/postgram'
              OR basename(o.project) = 'postgram'
              → retorna la fila histórica ✓
```

### Flujo de datos para proyecto sin git (basename fallback)

```
Agent input:  project = "mi-app"  (basename del directorio)
              directory = "/home/alice/projects/mi-app"
              scope = "personal"

Store write:  project = "mi-app"  ← normalizeProject() → sin modificación
              directory = "/home/alice/projects/mi-app"
```

### Tabla de transformaciones de `NormalizeRemoteURL`

| Input | Output |
|-------|--------|
| `https://github.com/owner/repo.git` | `github.com/owner/repo` |
| `https://github.com/owner/repo` | `github.com/owner/repo` |
| `http://github.com/owner/repo.git` | `github.com/owner/repo` |
| `git@github.com:owner/repo.git` | `github.com/owner/repo` |
| `git@github.com:owner/repo` | `github.com/owner/repo` |
| `https://user:token@github.com/owner/repo.git` | `github.com/owner/repo` |
| `https://gitlab.com/org/project.git` | `gitlab.com/org/project` |
| `git@bitbucket.org:team/repo.git` | `bitbucket.org/team/repo` |

### Tabla de transformaciones de `normalizeProject`

| Input | Output | Razón |
|-------|--------|-------|
| `github.com/owner/repo` | `github.com/owner/repo` | Ya normalizado, preservar |
| `/home/alice/projects/mi-app` | `mi-app` | Ruta Unix absoluta → basename |
| `C:\Users\bob\projects\mi-app` | `mi-app` | Ruta Windows absoluta → basename |
| `mi-app` | `mi-app` | Sin separadores → sin modificación |
| `` (vacío) | `unknown` | Fallback |
| `   ` (solo espacios) | `unknown` | Fallback |
| `https://github.com/owner/repo.git` | `github.com/owner/repo` | URL cruda → NormalizeRemoteURL |

---

## Correctness Properties

*A property is a characteristic or behavior that should hold true across all valid executions of a system — essentially, a formal statement about what the system should do. Properties serve as the bridge between human-readable specifications and machine-verifiable correctness guarantees.*

### Property 1: Convergencia de URLs HTTPS y SSH

*For any* triple `(host, owner, repo)` válido, construir la URL HTTPS (`https://host/owner/repo.git`) y la URL SSH (`git@host:owner/repo.git`) del mismo repositorio y pasarlas a `NormalizeRemoteURL` debe producir el mismo `Normalized_Project_Name` (`host/owner/repo`).

**Validates: Requirements 2.1, 2.2, 2.9**

### Property 2: Eliminación de credenciales embebidas

*For any* URL HTTPS con credenciales embebidas de la forma `https://user:token@host/owner/repo.git`, `NormalizeRemoteURL` debe producir un resultado que no contenga el usuario ni el token.

**Validates: Requirements 2.8**

### Property 3: Correctness de `normalizeProject` para los tres casos de entrada

*For any* valor de `project` recibido por el Store:
- Si es una ruta absoluta Unix o Windows (empieza con `/` o con letra de unidad `X:\`), `normalizeProject` debe retornar el último segmento del path.
- Si es un identificador ya normalizado con formato `host/owner/repo` (no empieza con `/` ni `\`, no contiene `\`, tiene al menos dos segmentos), `normalizeProject` debe retornarlo sin modificación.
- Si es un nombre simple sin separadores de path, `normalizeProject` debe retornarlo sin modificación.

**Validates: Requirements 7.1, 7.2, 7.7, 3.1, 3.2, 3.3**

### Property 4: Compatibilidad con datos históricos en consultas

*For any* nombre de proyecto normalizado `P`, si existen observaciones almacenadas con `project` igual a una ruta absoluta cuyo basename es el último segmento de `P`, una consulta filtrada por `P` debe retornar esas observaciones.

**Validates: Requirements 8.1, 8.2, 8.3, 8.5**

---

## Error Handling

| Situación | Comportamiento |
|-----------|---------------|
| `NormalizeRemoteURL` recibe URL malformada | Retorna `""` (el caller usa basename fallback) |
| `normalizeProject` recibe string vacío o solo espacios | Retorna `"unknown"` |
| `normalizeProject` recibe ruta con solo separadores (`/`, `\`) | Retorna `"unknown"` |
| `normalizeProject` recibe URL cruda (agente no normalizó) | Aplica `NormalizeRemoteURL` como defensa |
| Datos históricos con rutas absolutas en DB | Compatibilidad transparente vía normalización en consulta |

---

## Testing Strategy

### Enfoque dual

- **Tests de ejemplo**: casos concretos para cada transformación, edge cases, y paths de integración.
- **Tests de propiedad**: propiedades universales verificadas con [gopter](https://github.com/leanovate/gopter) (biblioteca de property-based testing para Go), mínimo 100 iteraciones por propiedad.

### Tests de propiedad (gopter)

Cada test de propiedad referencia la propiedad del diseño con un comentario:
```go
// Feature: project-identifier-normalization, Property N: <texto de la propiedad>
```

**Property 1 — Convergencia HTTPS/SSH**:
- Generador: triples `(host, owner, repo)` con strings alfanuméricos válidos
- Construir `https://host/owner/repo.git` y `git@host:owner/repo.git`
- Verificar: `NormalizeRemoteURL(https) == NormalizeRemoteURL(ssh) == "host/owner/repo"`

**Property 2 — Eliminación de credenciales**:
- Generador: triples `(user, token, host/owner/repo)` con strings alfanuméricos
- Construir `https://user:token@host/owner/repo.git`
- Verificar: el resultado no contiene `user` ni `token`

**Property 3 — Correctness de `normalizeProject`**:
- Sub-caso A (rutas absolutas): generador de paths Unix (`/a/b/c/name`) y Windows (`C:\a\b\name`)
  - Verificar: `normalizeProject(path) == filepath.Base(path)`
- Sub-caso B (nombres ya normalizados `host/owner/repo`): generador de triples sin `/` inicial ni `\`
  - Verificar: `normalizeProject("host/owner/repo") == "host/owner/repo"`
- Sub-caso C (nombres simples): generador de strings sin `/` ni `\`
  - Verificar: `normalizeProject(name) == strings.TrimSpace(name)` (si no vacío)

**Property 4 — Compatibilidad histórica** (test de integración con PostgreSQL real):
- Insertar observación con `project = "/home/user/projects/repo"`
- Consultar con `project = "repo"`
- Verificar: la observación aparece en los resultados

### Tests de ejemplo (tabla)

Ubicación: `internal/store/store_test.go` (normalización) y `internal/store/normalize_test.go` (funciones puras).

| Función | Input | Expected Output |
|---------|-------|----------------|
| `NormalizeRemoteURL` | `https://github.com/owner/repo.git` | `github.com/owner/repo` |
| `NormalizeRemoteURL` | `git@github.com:owner/repo.git` | `github.com/owner/repo` |
| `NormalizeRemoteURL` | `https://github.com/owner/repo` | `github.com/owner/repo` |
| `NormalizeRemoteURL` | `https://user:token@github.com/owner/repo.git` | `github.com/owner/repo` |
| `NormalizeRemoteURL` | `https://gitlab.com/org/project.git` | `gitlab.com/org/project` |
| `NormalizeRemoteURL` | `git@bitbucket.org:team/repo.git` | `bitbucket.org/team/repo` |
| `NormalizeRemoteURL` | `""` | `""` |
| `normalizeProject` | `github.com/owner/repo` | `github.com/owner/repo` |
| `normalizeProject` | `/home/alice/projects/mi-app` | `mi-app` |
| `normalizeProject` | `C:\Users\bob\projects\mi-app` | `mi-app` |
| `normalizeProject` | `mi-app` | `mi-app` |
| `normalizeProject` | `""` | `unknown` |
| `normalizeProject` | `"   "` | `unknown` |

### Comandos de validación

```bash
go test ./internal/store/... -v -run TestNormalize
go test ./internal/store/... -v -run TestProperty
go test ./... -cover
```
