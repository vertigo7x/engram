# Requirements Document

## Introduction

Postgram almacena el identificador de proyecto en las tablas `sessions`, `observations` y `user_prompts` como texto libre sin normalización. Los agentes pueden pasar rutas absolutas del sistema de archivos (ej. `/home/alice/projects/mi-app` o `C:\Users\bob\projects\mi-app`) en lugar de un identificador lógico consistente.

El problema central es que el **último segmento del path no es suficiente para unificar el proyecto entre usuarios**: dos usuarios pueden tener el mismo repositorio clonado en directorios con nombres distintos (ej. `mi-app` vs `postgram-fork`). El identificador verdaderamente universal e independiente de la máquina es la **URL del remote origin de git**.

Esta feature normaliza el identificador de proyecto usando la siguiente lógica de prioridad:

1. **Con git + remote origin**: usar la URL del remote origin normalizada (ej. `github.com/vertigo7x/postgram`) — identificador universal entre usuarios y máquinas.
2. **Con git sin remote origin**: usar el basename del directorio de trabajo como fallback, con scope `personal` automático.
3. **Sin git**: usar el basename del directorio de trabajo como fallback, con scope `personal` automático — un proyecto sin git es inherentemente local y no será compartido.

## Glossary

- **Project_Identifier**: El valor almacenado en las columnas `project` de las tablas `sessions`, `observations` y `user_prompts`.
- **Remote_Origin_URL**: La URL configurada como `origin` en el repositorio git del directorio de trabajo (ej. `https://github.com/vertigo7x/postgram.git`, `git@github.com:vertigo7x/postgram.git`).
- **Normalized_Project_Name**: Identificador limpio derivado del remote origin (ej. `github.com/vertigo7x/postgram`) o del basename del path como fallback (ej. `mi-app`).
- **Git_Repo**: Directorio de trabajo que contiene un repositorio git inicializado (tiene `.git/`).
- **Basename_Fallback**: Último segmento del path del directorio de trabajo, usado cuando no hay git o no hay remote origin.
- **Personal_Scope**: Scope `personal` asignado automáticamente cuando se usa el Basename_Fallback, indicando que el proyecto es local y no compartido.
- **Agent**: Cliente MCP que envía valores de `project` a Postgram (OpenCode, Claude Code, Gemini CLI, Codex, etc.).
- **Store**: Capa de datos en `internal/store/store.go` que persiste y consulta sesiones, observaciones y prompts.
- **Memory_Protocol**: Documentación en `DOCS.md` y `skills/memory-protocol/SKILL.md` que instruye a los agentes sobre cómo usar las herramientas MCP.
- **MCP_Handler**: Funciones en `internal/mcp/mcp.go` que procesan llamadas de herramientas MCP (`mem_save`, `mem_session_start`, etc.).
- **URL_Normalizer**: Función client-side (en el agente) que transforma una Remote_Origin_URL cruda en un Normalized_Project_Name limpio.

## Requirements

### Requirement 1: Detección del Remote Origin de Git por el Agente

**User Story:** Como agente MCP, quiero detectar el remote origin del repositorio git del directorio de trabajo, para que pueda derivar un identificador de proyecto universal antes de llamar a las herramientas MCP.

#### Acceptance Criteria

1. WHEN un agente inicia una sesión, THE Agent SHALL ejecutar `git remote get-url origin` (o equivalente) en el directorio de trabajo para obtener la Remote_Origin_URL
2. WHEN el comando git retorna una URL, THE Agent SHALL usar esa URL como base para derivar el Normalized_Project_Name
3. WHEN el directorio de trabajo no tiene git inicializado, THE Agent SHALL usar el Basename_Fallback como Normalized_Project_Name
4. WHEN el directorio de trabajo tiene git pero no tiene remote origin configurado, THE Agent SHALL usar el Basename_Fallback como Normalized_Project_Name
5. WHEN el Agent usa el Basename_Fallback, THE Agent SHALL asignar automáticamente scope `personal` a todas las observaciones de esa sesión
6. THE Agent SHALL detectar el remote origin una sola vez al inicio de la sesión y reutilizar el valor durante toda la sesión

### Requirement 2: Normalización de la URL del Remote Origin

**User Story:** Como agente MCP, quiero normalizar la URL del remote origin a un formato limpio y consistente, para que distintos formatos de URL del mismo repositorio produzcan el mismo identificador.

#### Acceptance Criteria

1. WHEN la Remote_Origin_URL tiene formato HTTPS (ej. `https://github.com/vertigo7x/postgram.git`), THE URL_Normalizer SHALL producir `github.com/vertigo7x/postgram`
2. WHEN la Remote_Origin_URL tiene formato SSH (ej. `git@github.com:vertigo7x/postgram.git`), THE URL_Normalizer SHALL producir `github.com/vertigo7x/postgram`
3. WHEN la Remote_Origin_URL termina en `.git`, THE URL_Normalizer SHALL eliminar el sufijo `.git`
4. WHEN la Remote_Origin_URL tiene prefijo `https://`, THE URL_Normalizer SHALL eliminar el prefijo `https://`
5. WHEN la Remote_Origin_URL tiene prefijo `http://`, THE URL_Normalizer SHALL eliminar el prefijo `http://`
6. WHEN la Remote_Origin_URL tiene formato `git@host:owner/repo`, THE URL_Normalizer SHALL transformar a `host/owner/repo`
7. THE URL_Normalizer SHALL preservar el host en el identificador resultante (ej. `github.com`, `gitlab.com`, `bitbucket.org`) para evitar colisiones entre repositorios con el mismo nombre en distintos hosts
8. WHEN la Remote_Origin_URL contiene credenciales embebidas (ej. `https://user:token@github.com/...`), THE URL_Normalizer SHALL eliminar las credenciales del identificador resultante
9. FOR ALL pares de URLs que referencian el mismo repositorio (HTTPS y SSH), THE URL_Normalizer SHALL producir el mismo Normalized_Project_Name (propiedad de convergencia)

### Requirement 3: Fallback a Basename con Scope Personal

**User Story:** Como usuario sin git en mi directorio de trabajo, quiero que Postgram use el nombre del directorio como identificador de proyecto, para que mis memorias se guarden aunque no tenga un repositorio git.

#### Acceptance Criteria

1. WHEN el Agent usa el Basename_Fallback, THE Agent SHALL extraer el último segmento del path del directorio de trabajo como Normalized_Project_Name
2. WHEN el path del directorio de trabajo es una ruta Unix absoluta (ej. `/home/alice/projects/mi-app`), THE Agent SHALL extraer `mi-app` como Normalized_Project_Name
3. WHEN el path del directorio de trabajo es una ruta Windows absoluta (ej. `C:\Users\bob\projects\mi-app`), THE Agent SHALL extraer `mi-app` como Normalized_Project_Name
4. WHEN el Normalized_Project_Name derivado del Basename_Fallback está vacío o contiene solo espacios, THE Agent SHALL usar el valor `"unknown"` como Normalized_Project_Name
5. WHEN el Agent usa el Basename_Fallback, THE Agent SHALL incluir en el campo `directory` de `mem_session_start` la ruta absoluta completa del directorio de trabajo para auditoría
6. WHEN el Agent usa el Basename_Fallback, THE Memory_Protocol SHALL indicar al agente que el scope por defecto para esa sesión es `personal`, ya que el proyecto es local y no compartido

### Requirement 4: Transmisión del Identificador al Servidor

**User Story:** Como desarrollador de Postgram, quiero que el agente detecte y normalice el project identifier client-side y lo envíe al servidor, para que el servidor reciba siempre un valor ya normalizado sin necesidad de inferirlo del campo `directory`.

#### Acceptance Criteria

1. THE Agent SHALL detectar y normalizar el Project_Identifier client-side antes de llamar a cualquier herramienta MCP
2. WHEN el Agent llama a `mem_session_start`, THE Agent SHALL enviar el Normalized_Project_Name en el parámetro `project` y la ruta absoluta completa en el parámetro `directory`
3. WHEN el Agent llama a `mem_save`, `mem_save_prompt`, o `mem_session_summary`, THE Agent SHALL enviar el mismo Normalized_Project_Name en el parámetro `project`
4. THE MCP_Handler SHALL aceptar el valor de `project` enviado por el agente sin derivarlo del campo `directory`
5. THE Store SHALL aplicar normalización server-side como capa de defensa adicional: WHEN el Store recibe un valor de `project` que contiene separadores de ruta (`/` o `\`), THE Store SHALL extraer el último segmento como fallback de seguridad

### Requirement 5: Actualización del Memory Protocol e Instrucciones al Agente

**User Story:** Como desarrollador de agentes, quiero que el Memory Protocol instruya explícitamente a los agentes sobre la lógica de detección y normalización del project identifier, para que los agentes envíen valores consistentes sin intervención manual.

#### Acceptance Criteria

1. WHEN un agente lee el Memory Protocol, THE Memory_Protocol SHALL incluir una sección "Project Identifier" que defina la lógica de detección en orden de prioridad: (1) remote origin git, (2) basename del path
2. THE Memory_Protocol SHALL especificar que WHEN hay remote origin, el agente debe ejecutar `git remote get-url origin` y normalizar la URL resultante
3. THE Memory_Protocol SHALL incluir ejemplos de normalización para los tres casos: con git+remote, con git sin remote, y sin git
4. THE Memory_Protocol SHALL indicar que WHEN se usa el Basename_Fallback, el scope por defecto de la sesión es `personal`
5. THE Memory_Protocol SHALL estar presente tanto en `DOCS.md` como en `skills/memory-protocol/SKILL.md`
6. THE Memory_Protocol SHALL incluir la tabla de transformaciones de URL: `https://github.com/owner/repo.git` → `github.com/owner/repo`, `git@github.com:owner/repo.git` → `github.com/owner/repo`

### Requirement 6: Actualización de Descripciones de Herramientas MCP

**User Story:** Como agente MCP, quiero que las descripciones de las herramientas MCP me indiquen cómo formatear el parámetro `project`, para que envíe valores consistentes sin depender de documentación externa.

#### Acceptance Criteria

1. WHEN un agente inspecciona las herramientas MCP, THE MCP_Handler SHALL incluir en la descripción del parámetro `project` la instrucción de usar el remote origin normalizado o el basename del path como fallback
2. THE MCP_Handler SHALL actualizar las descripciones de `project` en las herramientas `mem_save`, `mem_session_start`, `mem_session_summary`, `mem_save_prompt`, y `mem_context`
3. THE MCP_Handler SHALL incluir en la descripción de `mem_session_start` que el parámetro `directory` almacena la ruta local completa del workspace, mientras que `project` almacena el Normalized_Project_Name
4. THE MCP_Handler SHALL incluir un ejemplo de valor válido en cada descripción de parámetro `project` (ej. `"github.com/vertigo7x/postgram"` para proyectos con git, `"mi-app"` para proyectos sin git)
5. WHEN el agente usa el Basename_Fallback, THE MCP_Handler SHALL indicar en la descripción de `mem_session_start` que el scope recomendado es `personal`

### Requirement 7: Normalización Server-Side como Defensa en Profundidad

**User Story:** Como administrador de Postgram, quiero que el Store normalice automáticamente los valores de `project` recibidos como capa de defensa adicional, para que los datos históricos con rutas absolutas sigan siendo accesibles después del cambio.

#### Acceptance Criteria

1. WHEN el Store recibe un valor de `project` que contiene separadores de ruta (`/` o `\`), THE Store SHALL extraer el último segmento de la ruta como nombre normalizado
2. WHEN el Store recibe un valor de `project` sin separadores de ruta, THE Store SHALL usar el valor sin modificación
3. THE Store SHALL aplicar normalización en `CreateSession`, `AddObservation`, y `AddPrompt`
4. WHEN el Store normaliza un `project`, THE Store SHALL preservar el valor original en el campo `directory` de `sessions` para auditoría
5. THE Store SHALL aplicar normalización antes de cualquier filtro o comparación de `project` en consultas (`RecentSessions`, `RecentObservations`, `Search`, `RecentPrompts`)
6. WHEN el Store normaliza un `project` vacío o que contiene solo espacios, THE Store SHALL usar el valor `"unknown"` como fallback
7. WHEN el Store recibe un Normalized_Project_Name ya limpio (ej. `github.com/vertigo7x/postgram`), THE Store SHALL preservarlo sin modificación — la normalización server-side no debe alterar identificadores ya normalizados que contienen `/` como separador de host/owner/repo

### Requirement 8: Compatibilidad con Datos Existentes

**User Story:** Como administrador de Postgram con datos históricos, quiero que la normalización no rompa consultas existentes, para que los datos antiguos sigan siendo accesibles después del cambio.

#### Acceptance Criteria

1. WHEN el Store consulta observaciones con un `project` normalizado, THE Store SHALL retornar tanto entradas con el nombre normalizado como entradas históricas con rutas absolutas que normalizan al mismo nombre
2. THE Store SHALL aplicar normalización en ambos lados de la comparación: el valor de consulta y los valores almacenados
3. WHEN un usuario busca por `project = "mi-app"`, THE Store SHALL retornar entradas con `project = "mi-app"`, `project = "/home/alice/projects/mi-app"`, y `project = "C:\Users\bob\projects\mi-app"`
4. THE Store SHALL mantener los valores originales en la base de datos sin migración destructiva
5. THE Store SHALL aplicar normalización de forma transparente en tiempo de consulta

### Requirement 9: Documentar Normalización en DOCS.md

**User Story:** Como usuario de Postgram, quiero que la documentación explique cómo funciona la normalización de `project`, para que entienda por qué múltiples rutas y URLs se agrupan bajo el mismo identificador.

#### Acceptance Criteria

1. WHEN un usuario lee `DOCS.md`, THE Documentation SHALL incluir una sección "Project Identifier Normalization" en la sección "Features"
2. THE Documentation SHALL explicar la lógica de prioridad: (1) remote origin git normalizado, (2) basename del path con scope personal
3. THE Documentation SHALL incluir ejemplos de normalización para los tres casos: con git+remote (HTTPS y SSH), con git sin remote, y sin git
4. THE Documentation SHALL indicar que la normalización ocurre tanto en las instrucciones del agente (client-side) como en el servidor (server-side como defensa)
5. THE Documentation SHALL mencionar que el campo `directory` en `sessions` preserva la ruta original para auditoría
6. THE Documentation SHALL explicar por qué el basename del path no es suficiente para proyectos compartidos (dos usuarios pueden clonar en directorios con nombres distintos)

### Requirement 10: Actualizar Agent Setup Instructions

**User Story:** Como usuario configurando un agente, quiero que `docs/AGENT-SETUP.md` incluya la guía de normalización de `project`, para que configure correctamente mi agente desde el inicio.

#### Acceptance Criteria

1. WHEN un usuario lee `docs/AGENT-SETUP.md`, THE Documentation SHALL incluir una nota sobre normalización de `project` en la sección de Memory Protocol
2. THE Documentation SHALL indicar que los agentes deben derivar el `project` del remote origin git normalizado, con fallback al basename del directorio
3. THE Documentation SHALL incluir un ejemplo de configuración correcta vs incorrecta (ej. `"project": "github.com/org/repo"` vs `"project": "/home/alice/projects/repo"`)
4. THE Documentation SHALL indicar que proyectos sin git deben usar scope `personal`

### Requirement 11: Tests de Normalización

**User Story:** Como desarrollador de Postgram, quiero que los tests validen la normalización de `project` para todos los casos posibles, para que los cambios futuros no rompan la consistencia de identificadores.

#### Acceptance Criteria

1. THE URL_Normalizer_Tests SHALL incluir casos de prueba para URLs HTTPS con `.git` (ej. `https://github.com/owner/repo.git` → `github.com/owner/repo`)
2. THE URL_Normalizer_Tests SHALL incluir casos de prueba para URLs SSH (ej. `git@github.com:owner/repo.git` → `github.com/owner/repo`)
3. THE URL_Normalizer_Tests SHALL incluir casos de prueba para URLs HTTPS sin `.git` (ej. `https://github.com/owner/repo` → `github.com/owner/repo`)
4. THE URL_Normalizer_Tests SHALL incluir casos de prueba para URLs con credenciales embebidas (ej. `https://user:token@github.com/owner/repo.git` → `github.com/owner/repo`)
5. THE URL_Normalizer_Tests SHALL incluir casos de prueba para hosts distintos (ej. `gitlab.com`, `bitbucket.org`) verificando que el host se preserva en el resultado
6. FOR ALL pares (URL HTTPS, URL SSH) del mismo repositorio, THE URL_Normalizer_Tests SHALL verificar que ambas producen el mismo Normalized_Project_Name (propiedad de convergencia)
7. THE Store_Tests SHALL incluir casos de prueba para normalización de rutas Unix absolutas (ej. `/home/user/projects/repo` → `repo`)
8. THE Store_Tests SHALL incluir casos de prueba para normalización de rutas Windows absolutas (ej. `C:\Users\user\projects\repo` → `repo`)
9. THE Store_Tests SHALL incluir casos de prueba para valores ya normalizados sin separadores de ruta (ej. `repo` → `repo`)
10. THE Store_Tests SHALL incluir casos de prueba para valores vacíos o solo espacios (ej. `""` → `"unknown"`, `"   "` → `"unknown"`)
11. THE Store_Tests SHALL incluir casos de prueba para Normalized_Project_Names con formato `host/owner/repo` verificando que el Store los preserva sin modificación
12. THE Store_Tests SHALL validar que las consultas filtran correctamente por `project` normalizado después de insertar con rutas absolutas
