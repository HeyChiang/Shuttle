# Shuttle

Un gestor SSH de escritorio para trabajar con muchos servidores a la vez, creado con [Wails v3](https://v3.wails.io/), Go y Vue 3.

[English](README.md) | [简体中文](README.zh-CN.md) | Español

## Funciones

- **Hosts guardados** — perfiles de conexión (host, puerto, usuario, contraseña / clave privada, tipo de terminal, codificación) organizados por grupos.
- **Terminales multisesión** — abre tantas sesiones SSH como necesites, cambia entre pestañas, combínalas en una cuadrícula o envía la entrada a varias sesiones a la vez.
- **Espacio de trabajo SFTP** — administrador de archivos de dos paneles; cada panel puede explorar el disco local o un servidor remoto, con arrastrar y soltar para subir, descargar y transferir entre servidores.
- **Fragmentos de código** — guarda comandos de uso frecuente y ejecútálos o pégalos en la terminal activa desde un panel lateral.
- **Importar / exportar cifrado** — copia de seguridad y restauración de la lista de hosts en un archivo protegido con contraseña.
- **Temas y tipografía** — apariencia clara / oscura y tamaño de fuente de terminal ajustable.
- **Interfaz trilingüe** — English, chino simplificado y español, con cambio inmediato desde «Ajustes → Idioma».

## Requisitos

- Go 1.26+
- Node.js 18+ y npm
- La [CLI de Wails v3](https://v3.wails.io/getting-started/installation/) y [Task](https://taskfile.dev/)

## Desarrollo

```bash
# instalar dependencias del frontend
cd frontend && npm install && cd ..

# ejecutar en modo desarrollo con recarga en caliente
wails3 dev
```

## Compilación

```bash
wails3 build
```

El ejecutable se genera en `bin/`.

## Estructura del proyecto

```
.
├── main.go              # punto de entrada y configuración de la ventana
├── sshservice.go        # servicio Wails expuesto al frontend
├── internal/sshcore/    # sesiones SSH, SFTP, almacenamiento y cifrado
├── frontend/src/        # aplicación Vue 3
│   ├── App.vue          # ventana principal, pestañas, host, ajustes
│   ├── components/      # paneles de terminal, SFTP y diálogos
│   └── locales/         # catálogos de mensajes en / zh-CN / es
└── build/               # recursos de compilación por plataforma
```

## Internacionalización

Todos los textos de la interfaz están en `frontend/src/locales`. En el primer arranque se detecta el idioma del navegador y luego se guarda en `localStorage` con la clave `shuttle.locale`. Para añadir un idioma:

1. Copia `en.ts` como `frontend/src/locales/<locale>.ts` y traduce.
2. Regístralo en `frontend/src/i18n.ts` (`messages` y `localeOptions`).

## Contribuir

Las issues y pull requests son bienvenidas. Añade los textos nuevos a los catálogos de idiomas en lugar de escribirlos directamente en los componentes.
