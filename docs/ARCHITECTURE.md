# Arquitetura

O Selene separa a experiência interativa da lógica de sistema. Isso permite usar o mesmo núcleo pela TUI, pela CLI, por testes e, futuramente, por uma interface gráfica.

```text
CLI / TUI
   │
   ├── doctor (somente leitura)
   ├── catalog (manifestos embutidos e validados)
   ├── planner (plano somente leitura)
   ├── artifact (download, integridade e inspeção de ZIP)
   ├── installer (preflight e execução user-only)
   │      ├── setup.sh fixado do slsteam-moon
   │      └── ativação atômica de Lumen/LuaTools
   └── transaction
          ├── snapshot anterior à mutação
          ├── journal persistente
          └── rollback manual ou automático
```

## Limites atuais

A instalação real está limitada a Linux `amd64`, Steam nativa já inicializada e escopo do usuário. Steam Flatpak, Game Mode e integrações que exijam mudanças em `/usr` estão fora do primeiro adaptador.

## Fronteira de confiança

O Selene controla aquisição, hash, inspeção, staging, ambiente do processo, snapshot e rollback. O slsteam-moon continua sendo responsável pela sua lógica de wrapper `LD_AUDIT`, atalhos e guardian; essa lógica é executada a partir do `setup.sh` contido no artefato exato do catálogo.

O executor não baixa nem executa o `install.sh` vivo. Ele bloqueia `sudo`, força o modo imutável/user-only do upstream, remove `LD_AUDIT`, `LD_PRELOAD` e `LD_LIBRARY_PATH` herdados e valida os arquivos essenciais antes de commitar a transação.

O journal fica fora da árvore instalada. Assim, uma instalação interrompida continua recuperável com `selene rollback --yes`.
