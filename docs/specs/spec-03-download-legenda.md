# Spec 03 — Download e extração de legenda (yt-dlp encapsulado)

## Objetivo

Dado um pedido {link, início, fim}, baixar o vídeo do culto e a legenda automática do
YouTube pelo trecho da pregação, encapsulando o `yt-dlp` por baixo. A legenda passa
pelo srtclean e vira a transcrição que alimenta a seleção (spec-02).

## Contexto

O operador entrega link e os tempos de início/fim da pregação (julgamento humano). O
sistema precisa obter o material bruto sem que ninguém toque no terminal. O `yt-dlp`
é a ferramenta; ela fica escondida dentro do Go. Ver BRD (preparação da mídia) e a
DP-001 do BRD (sem transcrição local: se não houver legenda, o processo para).

## Escopo

Dentro:
- Pacote `internal/download` que chama o `yt-dlp` como subprocesso.
- Baixar a legenda automática (pt) e o vídeo, recortados ao intervalo {início, fim}.
- Passar a legenda pelo srtclean e gravar `trabalho/<id>/transcricao.txt`.
- Tratamento de erro explícito: vídeo indisponível, sem legenda pt, tempos inválidos.

Fora (specs futuras):
- Corte fino/render dos Shorts (spec-04) — aqui baixamos o trecho da pregação inteiro,
  não os cortes individuais.

## Decisões já tomadas (não reabrir)

- Sem transcrição local (Whisper etc.). Se o vídeo não tem legenda automática pt, o
  processo **para** com mensagem clara — não tenta transcrever. (BRD DP-001)
- srtclean já existe e é fiel; reusar. Só tempo de início; nunca alterar palavras.
- `yt-dlp` é dependência externa de sistema (não um módulo Go). Documentar no README
  como instalar; o Go só o invoca.
- Segredos/credenciais: nenhum necessário para vídeo público; nada hardcoded.

## Passos de implementação

1. `internal/download/ytdlp.go`: função que monta e executa o comando `yt-dlp` para
   baixar (a) a legenda automática pt e (b) o vídeo, limitados ao intervalo.
2. Validar os tempos {início, fim} antes de chamar (formato HH:MM:SS, fim > início).
3. Após baixar a legenda, chamar o srtclean (pacote `internal/transcricao` da spec-02)
   e gravar a transcrição em `trabalho/<id>/`.
4. Mapear os erros do `yt-dlp` para mensagens claras e para `Pedido.Status = erro`
   com `Pedido.Erro` preenchido.
5. Um comando `cmd/baixar` que, dado {link, início, fim}, executa e grava os artefatos —
   para testar a etapa isolada.
6. Testes: a chamada ao `yt-dlp` deve ser injetável (interface/execução mockável) para
   testar o fluxo sem baixar da internet. Testar o caminho "sem legenda → erro claro".

## Contratos e interfaces

`Baixar(ctx, pedido) error` — preenche `trabalho/<id>/legenda.srt`,
`trabalho/<id>/video.mp4` e `trabalho/<id>/transcricao.txt`; em falha, seta
`Status=erro` e `Erro`. A execução do `yt-dlp` fica atrás de uma interface
`Executor` para permitir mock nos testes.

Erros nomeados: `ErrSemLegenda`, `ErrVideoIndisponivel`, `ErrTempoInvalido`.

## Critérios de aceite

- [ ] `internal/download` invoca o `yt-dlp` atrás de uma interface mockável.
- [ ] Baixa legenda pt + vídeo do intervalo e gera `transcricao.txt` via srtclean.
- [ ] Sem legenda pt → `ErrSemLegenda`, `Status=erro`, mensagem clara; não transcreve.
- [ ] Tempos inválidos → `ErrTempoInvalido` antes de chamar o yt-dlp.
- [ ] Testes cobrem sucesso (mock) e o caminho sem-legenda, sem acessar a internet.
- [ ] README documenta a instalação do `yt-dlp`.
- [ ] `go build ./...` e `go test ./...` verdes.

## Como validar

```bash
go test ./...
# manual, com yt-dlp instalado e um vídeo real:
go run ./cmd/baixar -url "<link>" -inicio 00:05:30 -fim 00:38:10 -id teste
ls trabalho/teste/   # legenda.srt, video.mp4, transcricao.txt
```

## Fora de escopo / próximos passos

spec-04 — cortar cada candidato, reenquadrar 9:16 e queimar legenda (ffmpeg).
