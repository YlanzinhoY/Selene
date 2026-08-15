# Segurança

O Selene está em estágio inicial. `install --yes` modifica a integração da Steam no escopo do usuário; `fetch` escreve somente no cache.

## Relatando uma vulnerabilidade

Não abra publicamente detalhes que permitam exploração antes de uma correção. Entre em contato diretamente com o mantenedor pelo canal privado que será publicado junto da primeira release.

Inclua no relato:

- versão ou commit afetado;
- distribuição Linux e arquitetura;
- Steam nativa ou Flatpak;
- passos mínimos para reproduzir;
- impacto observado;
- logs sem tokens, credenciais ou dados pessoais.

## Regras da instalação

- downloads devem usar HTTPS e validação criptográfica;
- arquivos nunca devem ser extraídos fora do destino esperado;
- alterações precisam ser declaradas antes da confirmação;
- operações devem preferir o escopo do usuário;
- backups devem existir antes de qualquer substituição;
- falhas devem acionar rollback quando possível;
- nenhuma credencial da Steam deve ser lida ou armazenada.

O adaptador atual executa o `setup.sh` presente no ZIP fixado do slsteam-moon. Ele nunca executa o `install.sh` da branch `main`. Antes da execução, o Selene:

- confere tamanho e SHA-256 do artefato;
- rejeita ZIP inseguro e extrai em um staging privado;
- cria um snapshot persistente de todos os destinos conhecidos;
- recusa root e exige Steam nativa inicializada;
- remove variáveis `LD_*` herdadas;
- define `SLSM_IMMUTABLE=1` e `SLSM_SUDO_DENIED=1` para impedir alterações de sistema e prompts administrativos.

Snapshots podem conter cópias de configurações do próprio usuário. Eles ficam sob `XDG_STATE_HOME/selene/transactions` em diretórios privados e ainda não possuem expiração automática.

## Artefatos

O downloader atual:

- aceita somente URLs HTTPS de um catálogo embutido e validado;
- limita redirecionamentos e recusa downgrade para HTTP;
- confere o tamanho declarado;
- calcula SHA-256 enquanto recebe o arquivo;
- grava primeiro em arquivo temporário com permissão restrita;
- rejeita ZIPs com path traversal, links, entradas criptografadas, duplicatas ou expansão excessiva;
- confirma os arquivos obrigatórios antes de ativar o cache.

## Bootstrap do Selene

O `install.sh` da raiz instala somente o executável do Selene no escopo do usuário. Ele aceita apenas Linux `amd64`, recusa root, usa HTTPS inclusive nos redirecionamentos, exige o asset `.sha256`, executa `selene version` como autoteste e ativa o arquivo por rename no mesmo diretório.

O checksum publicado na mesma GitHub Release detecta corrupção ou troca isolada do binário, mas não substitui assinatura criptográfica e não protege contra comprometimento da conta/release. Assinatura dos artefatos permanece no roadmap. Recomenda-se baixar e revisar o bootstrap antes de executá-lo, como demonstrado no README.
