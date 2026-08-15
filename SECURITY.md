# Segurança

O Selene ainda está em estágio inicial e não realiza instalação nem modifica a Steam. O comando `fetch` escreve somente no cache do usuário.

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

## Artefatos

O downloader atual:

- aceita somente URLs HTTPS de um catálogo embutido e validado;
- limita redirecionamentos e recusa downgrade para HTTP;
- confere o tamanho declarado;
- calcula SHA-256 enquanto recebe o arquivo;
- grava primeiro em arquivo temporário com permissão restrita;
- rejeita ZIPs com path traversal, links, entradas criptografadas, duplicatas ou expansão excessiva;
- confirma os arquivos obrigatórios antes de ativar o cache.
