# Transações e rollback

Cada instalação e cada remoção completa abre uma transação antes de executar o primeiro processo upstream. O journal e os backups ficam em:

```text
${XDG_STATE_HOME:-$HOME/.local/state}/selene/transactions/<id>/
├── journal.json
├── backup/
└── stage/
```

O snapshot cobre:

- `~/.local/share/SLSsteam` e `~/.local/share/Lumen`;
- configuração e estado do slsteam-moon;
- `.bashrc`, `.zshrc` e `.profile`;
- entradas Steam do menu, autostart e Desktop, além do cache de associações regenerado no escopo do usuário;
- units, drop-ins e links de ativação do guardian no systemd do usuário;
- diretórios temporários usados na troca atômica do Lumen.

Arquivos que já existiam são copiados com seu tipo e permissões. Caminhos ausentes ficam registrados para serem removidos caso a instalação os crie. Globs estreitos registram a lista original, permitindo apagar atalhos novos e restaurar os anteriores.

## Estados

- `active`: snapshot concluído e instalação em andamento;
- `committed`: instalação validada; backup mantido para rollback manual;
- `rolling_back`: restauração em andamento;
- `rolled_back`: estado anterior restaurado;
- `failed`: erro registrado; o rollback pode ser repetido.

Uma falha durante a instalação chama o uninstaller do mesmo staging verificado, para o guardian e restaura o snapshot. Um rollback manual não executa scripts guardados: ele para diretamente os serviços conhecidos e restaura os dados do journal.

A remoção completa cria uma transação `uninstall` separada, baixa ou reutiliza do cache o slsteam-moon fixado, chama seu `setup.sh uninstall` e valida que o stack do usuário não deixou wrapper, tag de desktop, unit ou diretório de runtime. Se qualquer etapa falhar, essa transação restaura a instalação que existia antes da tentativa de remoção.

```bash
selene history
selene rollback --yes                 # transação recuperável mais recente
selene rollback --yes <transaction-id>
selene uninstall --yes
```

Em condições normais, `rollback` seleciona journals de instalação: seu significado é voltar ao estado anterior. Um journal de remoção interrompida também é recuperável para que uma queda de energia não deixe a integração pela metade. Depois de uma remoção completa concluída, o rollback não atravessa esse marco para reativar uma instalação antiga, nem mesmo quando um ID anterior é informado. `uninstall` tem significado diferente e remove o stack completo, mesmo quando Lumen ou slsteam-moon já existiam antes da instalação mais recente do Selene.

Depois de restaurar units anteriores, o Selene recarrega o systemd do usuário e reinicia apenas os timers/paths cujos links de ativação existiam no snapshot.

## Interrupções

Downloads podem ser cancelados sem rollback porque ocorrem antes do snapshot. Depois que a mutação começa, a TUI bloqueia a saída normal até commit ou rollback automático. Se houver queda de energia ou encerramento forçado durante uma instalação ou remoção, a transação permanece `active` e pode ser recuperada com `selene rollback --yes`.

Os snapshots ainda não são removidos automaticamente. Essa retenção favorece recuperação no MVP, mas pode consumir espaço proporcional à instalação anterior.
