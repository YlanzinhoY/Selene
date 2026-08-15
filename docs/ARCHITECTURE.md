# Arquitetura

O Selene separa a experiência interativa da lógica de sistema. Isso permite usar o mesmo núcleo pela TUI, pela CLI, por testes e, futuramente, por uma interface gráfica.

```text
CLI / TUI
   │
   ├── doctor (somente leitura)
   ├── manifest (planejado)
   ├── downloader (planejado)
   └── transaction (planejado)
          ├── backup
          ├── apply
          └── rollback
```

## Limites atuais

A primeira versão implementa apenas diagnóstico. Nenhum caminho detectado é modificado.

## Próximo limite arquitetural

A instalação deverá ser representada por um plano imutável antes de ser aplicada. A TUI apresentará esse plano, a CLI poderá serializá-lo e o executor registrará cada operação para permitir rollback.

O código de integração de cada componente não deverá baixar nem executar scripts arbitrários. Ele deverá produzir operações conhecidas, como criar diretório, baixar artefato verificado, extrair arquivo validado, criar link e gravar configuração.
