# Selene 🌙

> LuaTools no Linux, sem rituais de terminal.

Selene é um projeto comunitário e independente, escrito em Go, para facilitar o uso do ecossistema LuaTools no Linux. A proposta é reunir instalação, diagnóstico, atualização e rollback em uma experiência segura para quem joga pela Steam — inclusive com Proton.

O projeto está em fase inicial. A versão atual possui uma TUI e um diagnóstico **somente leitura** de plataforma, Steam, bibliotecas e Proton. Ela ainda não instala o LuaTools.

## Objetivos

- funcionar sem privilégios administrativos sempre que possível;
- explicar cada alteração antes de executá-la;
- verificar downloads com checksum ou assinatura;
- criar backup e oferecer rollback;
- reconhecer Steam nativa e Flatpak;
- oferecer uma experiência amigável no terminal;
- manter comandos não interativos para suporte e automação.

## Interface

Execute sem argumentos para abrir a TUI construída com [Bubble Tea](https://github.com/charmbracelet/bubbletea), [Lip Gloss](https://github.com/charmbracelet/lipgloss) e [Bubbles](https://github.com/charmbracelet/bubbles):

```bash
selene
```

O diagnóstico também pode ser executado diretamente:

```bash
selene doctor
selene doctor --json
selene version
```

## Desenvolvimento

Requisitos:

- Go 1.24 ou mais recente;
- Git;
- Linux para validar as integrações reais com Steam e Proton.

```bash
git clone URL_DO_SEU_REPOSITORIO/selene.git
cd selene
go mod download
go test ./...
go run ./cmd/selene
```

Compilar para Linux:

```bash
go build -o bin/selene ./cmd/selene
```

Cross-compilação a partir de Windows:

```powershell
$env:GOOS = "linux"
$env:GOARCH = "amd64"
go build -o bin/selene-linux-amd64 ./cmd/selene
Remove-Item Env:GOOS
Remove-Item Env:GOARCH
```

## Estrutura

```text
cmd/selene/       executável
internal/cli/     comandos não interativos
internal/doctor/  diagnóstico somente leitura
internal/ui/      interface Charm
internal/version/ informações de build
docs/             decisões e documentação técnica
```

## Segurança

Selene não executará scripts remotos com `curl | bash`. A instalação futura deverá usar downloads verificados, diretórios temporários protegidos, transações, backups e rollback automático.

Relate vulnerabilidades de forma responsável seguindo o arquivo [SECURITY.md](SECURITY.md).

## Roadmap

- [x] estrutura inicial em Go;
- [x] TUI com a stack Charm;
- [x] `selene doctor` e saída JSON;
- [x] detecção inicial de Steam nativa, Flatpak e Proton;
- [ ] testes reais no CachyOS;
- [ ] manifesto versionado dos componentes;
- [ ] download com SHA-256 e assinatura;
- [ ] instalação transacional;
- [ ] backup, rollback e desinstalação;
- [ ] integração com LuaTools Moon, slsteam-moon e Lumen;
- [ ] pacotes para GitHub Releases e AUR.

## Independência

Selene não possui afiliação oficial com Valve, Steam, LuaTools ou com os autores dos componentes que poderá integrar. Cada projeto mantém sua própria autoria e licença.

Antes da primeira publicação, o caminho do módulo em `go.mod`, a autoria e a licença do Selene deverão ser definidos pelo mantenedor.
