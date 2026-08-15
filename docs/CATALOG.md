# Catálogo e manifestos

Os manifestos do Selene são mantidos no próprio repositório. Eles não são baixados de uma URL arbitrária durante a instalação.

O catálogo estável atual fica em:

```text
internal/catalog/manifests/stable.json
```

Ele é incorporado ao binário com `go:embed` e validado quando o Selene inicia.

## Fontes oficiais

Cada componente deve apontar diretamente para uma fonte upstream conhecida:

- `swwayps/slsteam-moon`;
- `swwayps/lumen`;
- `swwayps/luatools-moon`;
- `swwayps/cloudredirect-moon`, somente para o componente opcional.

O `install.sh` do LuaTools Moon é uma referência para compreender as etapas, mas não é executado pelo Selene e não é uma fonte de integridade.

## Atualizando uma versão

1. Leia as notas e alterações da release upstream.
2. Confirme o repositório, tag e nome exato do artefato.
3. Obtenha o tamanho e o digest SHA-256 pela API oficial da release quando disponível.
4. Baixe o artefato e recalcule o SHA-256 localmente.
5. Liste todo o arquivo compactado e rejeite caminhos absolutos, `..`, links perigosos e conteúdo inesperado.
6. Confirme os marcadores usados por `install.validate`.
7. Atualize versão, URL, tamanho e SHA-256 juntos.
8. Execute `go test ./...`, `go vet ./...` e `selene plan --json luatools`.
9. Registre a mudança isoladamente, por exemplo:

```text
chore(catalog): update LuaTools stack to v2.9
```

Nunca use apenas o nome `latest` no catálogo estável. A URL deve conter uma tag ou commit imutável e o digest deve fazer parte do mesmo commit do manifesto.

## Estado dos adaptadores

- `extract`: conteúdo preparado e ativado em um destino do usuário;
- `replace-preserve`: substituição transacional com preservação dos dados declarados;
- `copy`: cópia verificada de um único arquivo;
- `verified-script`: executa um entrypoint que pertence ao próprio artefato fixado, somente depois de verificar e criar o snapshot.

O slsteam-moon usa `verified-script` porque seu projeto é a fonte da lógica que gera o wrapper `LD_AUDIT`, configura entradas desktop, PATH e serviços do usuário. O manifesto precisa declarar `setup.sh` simultaneamente como `entrypoint` e marcador de validação, com o argumento exato `install`. O adaptador Selene executa esse script com acesso administrativo bloqueado.

Os destinos de slsteam-moon e Lumen usam `${HOME}/.local/share`, não `${XDG_DATA_HOME}`: essa escolha reproduz os caminhos que o wrapper upstream resolve em runtime e evita que o plano prometa uma árvore diferente da realmente utilizada.
