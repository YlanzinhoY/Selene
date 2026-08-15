# Selene 🌙

> LuaTools no Linux, sem rituais de terminal.

Selene é um projeto comunitário e independente, escrito em Go, para facilitar o uso do ecossistema LuaTools no Linux. A proposta é reunir instalação, diagnóstico, atualização e rollback em uma experiência segura para quem joga pela Steam — inclusive com Proton.

O projeto está em fase inicial. A versão atual possui TUI, diagnóstico de plataforma/Steam/Proton, catálogo fixado por SHA-256 e instalação transacional do stack LuaTools. A integração real ainda precisa ser validada em uma máquina CachyOS antes da primeira release pública.

## Objetivos

- funcionar sem privilégios administrativos sempre que possível;
- explicar cada alteração antes de executá-la;
- verificar downloads com checksum ou assinatura;
- criar backup e oferecer rollback;
- reconhecer Steam nativa e Flatpak;
- oferecer uma experiência amigável no terminal;
- manter comandos não interativos para suporte e automação.

## Requisitos

Para instalar o LuaTools com o Selene você precisa de:

- Linux `x86_64/amd64`;
- Steam nativa, não a versão Flatpak;
- Steam aberta pelo menos uma vez para terminar o primeiro download/atualização;
- sessão normal do seu usuário, sem executar o Selene com `sudo`;
- internet para baixar os três artefatos verificados.

Proton e Wine não impedem o funcionamento. O Selene integra o LuaTools ao cliente Steam nativo; seus jogos continuam usando o Proton escolhido na Steam.

## 1. Instalando o Selene

Depois que uma release for publicada com o binário e seu arquivo `.sha256`, baixe e revise o bootstrap antes de executá-lo:

```bash
curl --proto '=https' --tlsv1.2 -fL \
  https://raw.githubusercontent.com/YlanzinhoY/Selene/main/install.sh \
  -o /tmp/selene-install.sh
less /tmp/selene-install.sh
sh /tmp/selene-install.sh
```

Ele instala somente o executável do Selene em `~/.local/bin/selene`, sem `sudo`. O binário baixado só é ativado depois de validar o SHA-256 publicado ao lado dele e passar no comando interno `version`.

Para uma versão específica ou para conferir tudo sem alterar arquivos:

```bash
sh install.sh --version v0.1.0
sh install.sh --dry-run --version v0.1.0
```

O bootstrap espera os assets `selene-linux-amd64` e `selene-linux-amd64.sha256`. No momento o repositório ainda não possui uma tag/release; portanto a publicação da primeira release continua sendo necessária para o download funcionar.

Para testar no CachyOS antes da primeira release, compile diretamente do repositório:

```bash
sudo pacman -S --needed go git
git clone https://github.com/YlanzinhoY/Selene.git
cd Selene
go test ./...
go build -trimpath -o selene ./cmd/selene
install -Dm755 selene "$HOME/.local/bin/selene"
```

O `sudo` acima serve somente para instalar Go/Git pelo pacman. `go test`, `go build`, `install` e o próprio Selene rodam como seu usuário normal.

Se o terminal responder `selene: command not found`, teste nesta sessão:

```bash
export PATH="$HOME/.local/bin:$PATH"
selene version
```

Depois, adicione a mesma linha ao `~/.bashrc` (Bash) ou `~/.zshrc` (Zsh) para torná-la permanente.

Para atualizar o Selene no futuro, execute o bootstrap novamente. O binário novo é verificado e testado antes de substituir o anterior.

## 2. Caminho mais fácil: interface interativa

Execute sem argumentos para abrir a TUI construída com [Bubble Tea](https://github.com/charmbracelet/bubbletea), [Lip Gloss](https://github.com/charmbracelet/lipgloss) e [Bubbles](https://github.com/charmbracelet/bubbles):

```bash
selene
```

Use as setas `↑`/`↓` para navegar, `Enter` para selecionar, `Esc` para voltar e `q` para sair. Durante uma instalação ou rollback a saída fica bloqueada até a transação terminar com segurança.

As opções da TUI são:

- **Diagnosticar ambiente:** verifica Linux, Steam, bibliotecas e Proton sem alterar arquivos.
- **Planejar instalação:** mostra downloads, hashes, destinos e riscos.
- **Baixar e verificar:** deixa os pacotes prontos no cache, mas não instala nada.
- **Instalar LuaTools:** apresenta uma segunda tela de confirmação, cria o snapshot e instala.
- **Desfazer instalação:** restaura a transação recuperável mais recente.
- **Sobre o Selene:** mostra a missão e o estado do projeto.

Fluxo recomendado na primeira vez:

1. Abra **Diagnosticar ambiente**.
2. Corrija qualquer item marcado como erro.
3. Abra **Planejar instalação** e revise os destinos.
4. Use **Baixar e verificar** se quiser preparar o cache antecipadamente.
5. Selecione **Instalar LuaTools** e leia a confirmação.
6. Depois do sucesso, abra a Steam normalmente.

## 3. Todos os comandos

Tudo que existe na TUI também pode ser usado diretamente no terminal:

| Comando | O que faz | Altera a instalação? |
|---|---|---:|
| `selene` | Abre a interface interativa. | Somente após confirmação |
| `selene doctor` | Diagnostica Linux, Steam nativa/Flatpak, bibliotecas e Proton. | Não |
| `selene doctor --json` | Mesmo diagnóstico em JSON para suporte ou automação. | Não |
| `selene catalog` | Lista o bundle e os artefatos fixados no binário. | Não |
| `selene catalog --json` | Exibe catálogo, URLs, tamanhos e SHA-256 em JSON. | Não |
| `selene plan` | Mostra cada operação planejada e seus destinos. | Não |
| `selene plan --json luatools` | Emite o plano do bundle `luatools` em JSON. | Não |
| `selene fetch` | Baixa, verifica e inspeciona os pacotes no cache. | Somente o cache |
| `selene fetch --json luatools` | Faz o mesmo e retorna os resultados em JSON. | Somente o cache |
| `selene install` | Mostra novamente o plano e como confirmar. | Não |
| `selene install --yes` | Encerra Steam/Lumen, cria snapshot e instala o LuaTools. | Sim |
| `selene history` | Lista IDs e estados das transações. | Não |
| `selene history --json` | Lista os journals completos em JSON. | Não |
| `selene rollback --yes` | Restaura a transação recuperável mais recente. | Sim |
| `selene rollback --yes ID` | Restaura uma transação específica. | Sim |
| `selene version` | Mostra versão, commit e data do build. | Não |
| `selene help` | Mostra a ajuda resumida. | Não |

### Instalação somente pelo terminal

```bash
# 1. Diagnóstico
selene doctor

# 2. Revisão sem mudanças
selene plan

# 3. Download opcional antecipado
selene fetch

# 4. Instalação confirmada
selene install --yes
```

O instalador não pede `sudo`. Ele executa somente o `setup.sh` presente na release fixada do slsteam-moon, com alterações de sistema bloqueadas. Se qualquer etapa falhar, o rollback automático tenta devolver todos os caminhos ao estado anterior.

### Consultando e desfazendo

```bash
selene history
selene rollback --yes

# Para escolher um snapshot da lista:
selene rollback --yes 20260815T120000Z-exemplo
```

O rollback para o guardian, restaura arquivos, diretórios, atalhos, configurações e links de ativação do systemd. Ele não baixa nada e não reexecuta scripts guardados no snapshot.

## 4. Primeiro teste no CachyOS

No CachyOS, a forma recomendada pelo próprio projeto é abrir **CachyOS Hello → Apps/Tweaks → Install Gaming packages**. Pelo terminal, os metapacotes correspondentes são:

```bash
sudo pacman -S cachyos-gaming-meta cachyos-gaming-applications
```

O `cachyos-gaming-applications` inclui a Steam. O Selene em si e a instalação do LuaTools devem ser executados depois como usuário normal, sem `sudo`. Consulte o [guia oficial de jogos do CachyOS](https://wiki.cachyos.org/configuration/gaming/) e, para detalhes da Steam no ecossistema Arch, a [documentação da ArchWiki](https://wiki.archlinux.org/title/Steam).

Checklist para o teste:

1. Atualize o CachyOS e instale os pacotes de jogos.
2. Abra a Steam nativa, faça login e espere a primeira atualização terminar.
3. Feche a Steam.
4. Instale o binário do Selene.
5. Rode `selene doctor` e confirme que **Steam nativa** foi encontrada.
6. Rode `selene plan` e confira que o destino é `~/.local/share`.
7. Abra `selene` para testar a TUI ou rode `selene install --yes`.
8. Guarde o ID da transação mostrado no final.
9. Abra a Steam normalmente e verifique se Lumen/LuaTools carregaram.
10. Se algo der errado, feche a Steam e rode `selene rollback --yes`.

Para relatar o primeiro teste, anote a edição do CachyOS, ambiente gráfico, GPU/driver, saída de `selene doctor`, ID da transação e o log exibido. Remova nomes de usuário, tokens ou outros dados pessoais antes de publicar.

## 5. Onde o Selene grava arquivos

| Conteúdo | Caminho padrão |
|---|---|
| Executável do Selene | `~/.local/bin/selene` |
| Cache de downloads | `~/.cache/selene/downloads` |
| Journals e snapshots | `~/.local/state/selene/transactions` |
| slsteam-moon | `~/.local/share/SLSsteam` |
| Lumen e LuaTools | `~/.local/share/Lumen` |
| Configuração SLSsteam | `~/.config/SLSsteam` |

As variáveis XDG são respeitadas para cache, configuração, estado, atalhos e serviços. Os runtimes SLSsteam e Lumen permanecem em `~/.local/share` porque esse é o caminho que o wrapper upstream utiliza.

## 6. Problemas comuns

### `selene: command not found`

Adicione `~/.local/bin` ao `PATH`, conforme mostrado na seção de instalação.

### Steam nativa não inicializada

Abra a Steam pelo menu, espere o cliente terminar de baixar/atualizar e feche-o. Depois rode `selene doctor` novamente.

### Somente Steam Flatpak encontrada

O primeiro adaptador do Selene ainda não instala na Steam Flatpak. No CachyOS, use a Steam nativa incluída nos pacotes de jogos.

### O instalador foi interrompido

Rode `selene history`. Uma transação `active` ou `failed` pode ser recuperada com `selene rollback --yes`.

### Quero remover apenas o executável do Selene

Primeiro faça rollback do LuaTools se também quiser desfazer a integração. Depois, remova `~/.local/bin/selene`. Apagar somente o executável não remove LuaTools, Lumen nem os snapshots.

O catálogo está em [`internal/catalog/manifests/stable.json`](internal/catalog/manifests/stable.json). Ele fixa as releases `v2.8` atuais de slsteam-moon, Lumen e LuaTools Moon, incluindo URL HTTPS, tamanho e SHA-256 de cada pacote. Consulte [`docs/CATALOG.md`](docs/CATALOG.md) antes de atualizá-lo.

## Desenvolvimento

Requisitos:

- Go 1.24 ou mais recente;
- Git;
- Linux para validar as integrações reais com Steam e Proton.

```bash
git clone https://github.com/YlanzinhoY/Selene.git
cd Selene
go mod download
go test ./...
sh scripts/test-install.sh
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
internal/artifact/ download, SHA-256 e inspeção segura dos pacotes
internal/catalog/ catálogo embutido e validação dos manifestos
internal/doctor/  diagnóstico somente leitura
internal/planner/ plano de instalação auditável
internal/installer/ executor user-only e integração upstream
internal/transaction/ snapshot, journal e rollback
internal/ui/      interface Charm
internal/version/ informações de build
docs/             decisões e documentação técnica
```

## Segurança

Selene não executa `curl | bash` nem busca um instalador mutável em `main`. O downloader usa HTTPS, confere tamanho e SHA-256 durante o streaming e inspeciona os ZIPs antes de aceitá-los. O `setup.sh` executado vem de uma release fixada do slsteam-moon, é extraído em staging privado e roda com `SLSM_IMMUTABLE=1`, `SLSM_SUDO_DENIED=1` e variáveis de injeção removidas.

Antes de executar esse setup, o Selene salva os caminhos afetados. Falhas acionam o uninstaller verificado, param os serviços criados e restauram o snapshot. Um commit bem-sucedido mantém os dados de rollback em `XDG_STATE_HOME/selene/transactions`. Consulte [docs/TRANSACTIONS.md](docs/TRANSACTIONS.md).

Relate vulnerabilidades de forma responsável seguindo o arquivo [SECURITY.md](SECURITY.md).

## Roadmap

- [x] estrutura inicial em Go;
- [x] TUI com a stack Charm;
- [x] `selene doctor` e saída JSON;
- [x] detecção inicial de Steam nativa, Flatpak e Proton;
- [x] catálogo estável com artefatos reais e SHA-256;
- [x] `selene plan` com operações, destinos e bloqueios;
- [x] cache com download verificado e inspeção segura de ZIP;
- [ ] testes reais no CachyOS;
- [ ] assinatura do catálogo e das releases;
- [x] instalação user-only transacional;
- [x] backup, journal e rollback manual/automático;
- [x] integração fixada com LuaTools Moon, slsteam-moon e Lumen;
- [ ] suporte à Steam Flatpak;
- [ ] política de retenção/limpeza de snapshots;
- [ ] pacotes para GitHub Releases e AUR.

## Independência

Selene não possui afiliação oficial com Valve, Steam, LuaTools ou com os autores dos componentes que poderá integrar. Cada projeto mantém sua própria autoria e licença.

Antes da primeira publicação, o caminho do módulo em `go.mod`, a autoria e a licença do Selene deverão ser definidos pelo mantenedor.
