# Segurança

O Selene ainda está em estágio inicial e não realiza instalação nem modifica a Steam.

## Relatando uma vulnerabilidade

Não abra publicamente detalhes que permitam exploração antes de uma correção. Entre em contato diretamente com o mantenedor pelo canal privado que será publicado junto da primeira release.

Inclua no relato:

- versão ou commit afetado;
- distribuição Linux e arquitetura;
- Steam nativa ou Flatpak;
- passos mínimos para reproduzir;
- impacto observado;
- logs sem tokens, credenciais ou dados pessoais.

## Regras para futuras instalações

- downloads devem usar HTTPS e validação criptográfica;
- arquivos nunca devem ser extraídos fora do destino esperado;
- alterações precisam ser declaradas antes da confirmação;
- operações devem preferir o escopo do usuário;
- backups devem existir antes de qualquer substituição;
- falhas devem acionar rollback quando possível;
- nenhuma credencial da Steam deve ser lida ou armazenada.
