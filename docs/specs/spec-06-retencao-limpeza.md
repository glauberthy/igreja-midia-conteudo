# Spec 06 — Retenção do bruto e limpeza de disco

> **Revisada em 2026-07-29 pela v3 da spec-05 (cache por vídeo).** A premissa desta spec era
> "o vídeo é descartável assim que os Shorts existem". Com o cache por vídeo, o vídeo passou a
> ser **reutilizável entre pedidos** — apagá-lo ao concluir joga fora exatamente o que o cache
> existe para guardar. A política do vídeo mudou; a dos artefatos de pedido não.
> Ver "Revisão de 2026-07-29" no fim.

## Objetivo

Evitar que o disco encha: descartar o vídeo bruto (o arquivo grande) dos pedidos
anteriores, mantendo o histórico auditável (texto, candidatos validados, Shorts) e o
bruto do pedido mais recente. Fecha o ciclo operacional.

## Contexto

O vídeo baixado do culto é o maior consumidor de disco e só serve para gerar os
Shorts. Depois que os finalizados existem, o bruto pode ir embora. Texto (transcrição,
candidatos) e logs são leves e úteis para auditoria. Ver BRD DP-007 (vídeo retido só
enquanto necessário; texto/logs retidos). O conceito de "sermão em aberto" do BRD
nunca foi formalizado; aqui adotamos uma regra concreta e simples no lugar.

## Escopo

Dentro:
- Descartar `trabalho/<id>/video.mp4` e demais brutos dos pedidos ANTERIORES, mantendo
  intactos os N mais recentes (padrão 1) — ver a política em "Decisões".
- Manter `transcricao.txt`, `candidatos.corrigido.json`, `revisao-teologica.json`,
  `pedido.json` e tudo em `finalizados/` e `resultados/`.
- Duas formas de acionar: automática (ao concluir um pedido) e manual (`cmd/limpar`,
  com `-dry-run`).

Fora:
- Qualquer política jurídica de retenção de dados (não se aplica; culto é público —
  decisão já registrada no BRD). Aqui é só higiene de disco.

## Decisões já tomadas (não reabrir)

- O bruto é descartável assim que os finalizados existem. (BRD DP-007)
- Texto e logs são retidos.
- "Sermão em aberto" (conceito abstrato do BRD, nunca definido) é substituído por uma
  regra concreta.
- **A política é por CONTAGEM DE PEDIDOS, não por prazo** (mudança em relação ao rascunho
  desta spec, que falava em 7 dias). Mantém-se o bruto dos N pedidos mais recentes
  (padrão N=1) e limpa-se o resto. Motivo: com ~571 MB por pedido (medido), um prazo de 7
  dias significa "quantos pedidos couberem em 7 dias" — imprevisível, porque depende da
  frequência de uso. Por contagem o teto é conhecido — mas **o teto é N × o MAIOR pedido,
  não N × a média**: os vídeos medidos variaram de 124 MB a 994 MB (8x), porque a
  resolução disponível no YouTube varia. Com N=1, `trabalho/` fica entre ~124 MB e ~1 GB
  conforme o último pedido; hoje calhou de ser o menor (124 MB), o que faz o resultado
  parecer melhor do que o pior caso. Dimensionar disco por **~1 GB por pedido retido**.
  Manter o último permite regerar um Short sem baixar de novo; mais que isso volta a
  acumular.
- **A recência vem dos arquivos PRESERVADOS do pedido, não do mtime da pasta.** Apagar
  arquivos atualiza o mtime do diretório — se a ordem viesse dali, o pedido recém-limpo
  viraria "o mais recente" e a limpeza seguinte comeria justamente o que deve ser retido.
  (Bug real, pego por teste durante a implementação.)

- **Verificação de espaço ANTES da fase pesada (parte PROSPECTIVA).** A limpeza sozinha é
  reativa: arruma depois. Mas a falha que esta spec existe para evitar acontece ANTES — o
  disco enchendo no meio de um download de ~900 MB, com o yt-dlp morrendo com um erro de
  biblioteca que não diz nada ao operador. Então, antes de baixar: confere o espaço livre;
  se estiver abaixo da margem (`MargemPadrao` = 2 GB — um vídeo passa de 900 MB, e ainda há
  o merge do yt-dlp e os Shorts), roda a limpeza automática; se AINDA faltar, falha ali
  mesmo com os números ("espaço insuficiente: N livres, precisa de ~M"). Falhar cedo com
  diagnóstico é muito melhor que falhar no meio sem explicação.
- **O pedido que FALHA é limpo na hora** (não espera a política): ele não tem Short a
  regerar e pode ter deixado mp4 parcial, `.part`, `.ytdl`. Como a falha costuma acontecer
  justamente com o disco apertado, deixar esse resíduo realimentaria o problema — falhas
  acumulariam lixo que nunca seria limpo.

## Passos de implementação

1. `internal/retencao/limpeza.go`: aplica a política e remove o bruto; preserva o
   histórico auditável.
2. Retenção configurável (`-reter`, padrão 1 pedido).
3. `cmd/limpar` (manual/cron) com `-dry-run`, que reporta o que removeu e quanto liberou.
4. Acionamento automático ao final de cada pedido concluído (no servidor), mantendo o
   pedido atual como intocável.
5. Testes: política, whitelist de preservação, guarda de caminho, dry-run, idempotência.

## Contratos e interfaces

`retencao.Limpar(Opcoes{RaizTrabalho, Reter, Intocaveis, DryRun}) (Resultado, error)` —
remove os brutos elegíveis e devolve o que foi removido, os bytes liberados e os retidos.
Idempotente (rodar de novo não quebra nem re-conta).

**Apagados** (bruto regenerável): `video.mp4`, `legenda.srt`, `legenda.info.json`,
`short_NN.subNNN.txt`, `mapa.json`, `candidatos_brutos.json`, `candidatos_delim.json`,
`*.part`/`*.ytdl`.

**Preservados SEMPRE** (histórico auditável): `candidatos.corrigido.json` (fonte de verdade
validada, spec-09), `transcricao.txt` (insumo do `cmd/auditar`), `revisao-teologica.json`
(spec-14), `pedido.json`. E, por construção, tudo em `finalizados/` e `resultados/` — que
estão FORA da raiz de trabalho, a única pasta que a limpeza enxerga.

**Segurança:** a checagem de preservados vence a de removíveis (se alguém puser um arquivo
protegido na lista de remoção, ele continua protegido — coberto por teste); `caminhoSeguro`
recusa travessia (`..`, separadores, caminho absoluto) e confere que o destino está sob a
raiz; o pedido em curso entra como intocável.

### A invariante do pedido em curso é estrutural, não verificada

Uma corrida na limpeza não corrompe uma leitura: ela **apaga arquivo, em silêncio**. O
cenário perigoso é a limpeza automática rodando concorrente com um pedido que está
começando e removendo o `video.mp4` que ele acabou de baixar. Um `-race` verde não é prova
de ausência disso — o detector só vê as corridas que aconteceram naquela execução, e
"arquivo apagado indevidamente" nem é o tipo de falha que ele reporta.

Por isso a garantia é **de construção**, não de verificação posterior:

- O conjunto de intocáveis é calculado por `intocaveisLocked`, que percorre os pedidos em
  memória e protege **todo** pedido que não chegou a estado terminal (`concluido`/`erro`) —
  não apenas o pedido que acabou de concluir.
- Esse cálculo e a **remoção** acontecem com o **mesmo mutex** que registra pedido novo
  (`handleCriar`) e muda estado (`setStatus`/`setErro`) segurado do começo ao fim. Logo
  nenhum pedido pode nascer nem avançar entre a decisão e o `os.Remove`: não existe janela
  em que um pedido em curso fique invisível para a lista.
- Vale para **as três** entradas que apagam — limpeza automática, `GarantirEspaco`
  (que também limpa quando falta margem) e a limpeza de resíduo de erro. Todas passam pelo
  mesmo ponto de montagem, para não divergirem.

Custo: desprezível. A remoção é de poucos arquivos e o servidor atende um pedido por vez.

Provado por três testes: a invariante direta (pedido em curso é o mais antigo do disco e
sobrevive, com contraprova de que a limpeza de fato removeu algo naquela rodada), o lado
oposto (estado terminal É limpável, senão o disco nunca esvazia) e um teste de estresse
com pedidos nascendo enquanto a limpeza roda — este verifica **sobrevivência do arquivo**,
não só ausência de corrida de memória.

### O outro lado: "em curso" precisa sempre acabar

Proteger todo pedido não-terminal cria uma dependência nova: se um pedido pudesse ficar
travado, ele seria **imortal para a limpeza**. Um `yt-dlp` em stall de rede deixaria o
pedido em `baixando-video` para sempre — os ~900 MB dele protegidos permanentemente, a
fila bloqueada (um pedido por vez) e o operador com um spinner infinito, exatamente o que
a spec-05 quis evitar. Duas coisas seguram isso.

**1. Prazo por etapa (`internal/servidor/prazos.go`).** Toda etapa roda sob
`context.WithTimeout`, então o estado sempre termina:

| Etapa | Prazo | Referência medida |
|---|---|---|
| Legenda | 10 min | poucos MB |
| Seleção | 30 min | ~50s (o harness faz várias chamadas ao modelo) |
| Render | 15 min | ~3s por Short |
| **Vídeo** | **5 min sem progresso** (teto de 2 h) | ver abaixo |

Nas três primeiras a folga é de mais de dez vezes o medido, então rede lenta **mas viva**
não é interrompida; só a TRAVADA é.

**O download é dimensionado por progresso, não por tempo.** Ali o pior caso não é o máximo
medido: é o produto de duas variáveis que já vimos variar muito — tamanho (maior visto:
994 MB; um culto de 2 h daria ~1,8 GB) e throughput (3,3 a 23,4 MB/s, uma razão de 7×).
1,8 GB a 3,3 MB/s levam ~9 minutos, então um teto fixo de 15–20 min daria margem de ~2×, e
não 10× — apertado o bastante para matar um download legítimo de culto longo em rede ruim.

Medir progresso é **imune ao tamanho do arquivo**: um download vivo, mesmo a 10 KB/s,
escreve alguma coisa a cada minuto; só o travado fica 5 minutos sem escrever nada. O
watchdog soma o tamanho da pasta do pedido (e não de um arquivo só, porque o yt-dlp baixa
em 8 fragmentos paralelos). O teto de 2 h continua existindo apenas como rede de segurança
contra patologia do próprio watchdog. As duas condições geram mensagens diferentes —
"ficou 5min sem baixar nada" e "passou de 2h" são diagnósticos distintos para o operador.

O prazo só funciona porque o `ctx` é
propagado até a syscall nos três caminhos: download e render via `exec.CommandContext`
(que mata o processo), e o modelo via `http.NewRequestWithContext`. O backoff do download
já checava o `ctx`; o do harness passou a abortar cedo em vez de gastar as tentativas
restantes a seco. Quando o prazo estoura, a mensagem nomeia a etapa e o tempo, e não é
prefixada com "falha na seleção:" — ela já se explica.

**1b. O cancelamento mata o GRUPO de processos (`internal/processo`).** Um prazo que só
"interrompe" não basta. O yt-dlp cria um `ffmpeg` filho; `exec.CommandContext` mata apenas
o processo direto, e o neto sobrevive. Isso quebra a limpeza de um jeito silencioso: no
Linux, apagar um arquivo que um processo mantém aberto remove o nome mas **não devolve os
blocos** — só quando o descritor fecha. O `GarantirEspaco` reportaria "liberei 900 MB" e o
`df` continuaria cheio, fazendo o pedido seguinte falhar por espaço que deveria existir.

Há ainda uma armadilha do `os/exec`: com `Stdout` num `bytes.Buffer`, o `Wait()` só retorna
quando todos os escritores do pipe fecham — **inclusive os netos**. Um neto vivo trava o
`Wait()` e o prazo não resolve nada. Medido na contraprova: 8,3 s em vez de 0,3 s.

Os dois executores (download e render) passaram a delegar a `internal/processo`, que:

- põe o comando num grupo próprio (`Setpgid`) e faz o cancelamento enviar `SIGKILL` ao
  grupo inteiro (PID negativo), alcançando os filhos;
- define `WaitDelay` como rede contra o pipe preso;
- usa `cmd.Run()` (Start + Wait), então **quando `Rodar` retorna o processo já foi
  colhido** — a limpeza de resíduo que vem depois nunca corre contra um processo vivo.

**Guarda anti-suicídio.** `kill(-pgid, SIGKILL)` num pgid herdado do servidor mataria o
**próprio servidor** — falha catastrófica e quase impossível de diagnosticar (o serviço
morre ao cancelar um download). Antes de sinalizar o grupo, `alvoDoKill` confere via
`Getpgid` que o grupo do filho não é o nosso **e** que o filho lidera o próprio grupo; em
qualquer dos dois casos degrada para matar só o PID direto e emite aviso. Perder um neto é
ruim (espaço preso); derrubar o serviço é pior; falhar em silêncio é o pior de todos. A
decisão é uma função pura, testada exaustivamente — inclusive uma asserção de que nenhuma
combinação de PIDs jamais produz o grupo do servidor como alvo.

Provado por teste com contraprova: sem o kill de grupo, o neto sobrevive e a remoção
devolve **0 MB de 60 MB**. Duas correções de método aqui valem registro, porque as
primeiras versões passavam **com o bug presente**:

- medir a presença do arquivo não serve — o `unlink` sempre remove o nome. O que se mede é
  `statfs`.
- medir `statfs` em valor absoluto (livre no início vs. no fim) também não serve numa
  máquina em uso: o experimento real acusou "75 MB presos" com resíduo de 9 MB, ou seja,
  mediu a atividade normal do sistema durante 33 s. O correto é o **delta da remoção**
  (livre imediatamente antes vs. imediatamente depois), com janela de milissegundos.

### Verificado contra um yt-dlp real, não só contra fakes

Prazo e kill de grupo nasceram testados apenas com dublês. A lacuna foi fechada sem
simular rede ruim: **`kill -STOP` reproduz "travado" exatamente** — o processo para de
escrever e não morre. É também o caso que distingue os sinais: a um processo parado o
SIGTERM não é entregue, o SIGKILL é.

`TestExperimentoRealDownloadTravado` (em `internal/servidor`, pulado por padrão) baixa um
vídeo de verdade, congela a árvore do yt-dlp com SIGSTOP no grupo depois dos primeiros MB e
verifica os três elos. Resultado medido:

| Elo | Resultado |
|---|---|
| Watchdog dispara por falta de progresso | abortou em 30 s ("ficou 25s sem baixar nada") |
| Grupo morre, mesmo `STOPPED` | nenhum `yt-dlp`/`ffmpeg` vivo depois |
| Espaço devolvido | 8 MB devolvidos de 7 MB de resíduo; nenhum descritor aberto em `/proc` |

Para repetir:

```bash
SHORTS_EXPERIMENTO_REAL=1 SHORTS_EXPERIMENTO_URL=<url> \
  go test -run ExperimentoReal -timeout 10m -v ./internal/servidor/
```

**2. Autocura no reinício.** O estado dos pedidos vive **só em memória**: o servidor não
grava `pedido.json` (só o `cmd/baixar` grava) e `Novo()` não lê nada do disco. Um pedido
travado por crash ou `kill -9` — onde o prazo não chega a rodar — some do mapa no
restart, e o material bruto dele volta a ser limpável.

Consequência prática: **pedido travado é chateação, não vazamento de disco.** No pior caso
o operador reinicia o servidor. Se algum dia o estado passar a ser persistido, esta
propriedade se perde e a limpeza vai precisar de um critério de idade para pedidos
"em curso" — está registrado aqui porque é uma dependência silenciosa.

## Critérios de aceite

- [x] Bruto removido dos pedidos anteriores; os N mais recentes ficam intactos.
- [x] `candidatos.corrigido.json`, `transcricao.txt`, `revisao-teologica.json` e
      `pedido.json` preservados; `finalizados/` nunca tocado.
- [x] Nenhum pedido em curso pode ser enxergado pela limpeza — garantido pela estrutura
      (mesmo mutex cobrindo decisão e remoção), não por checagem posterior.
- [x] Todo pedido chega a estado terminal: prazo por etapa com `ctx` propagado até a
      syscall, e autocura no reinício para o que o prazo não alcança (crash).
- [x] Retenção configurável (`-reter`, padrão 1).
- [x] `cmd/limpar` roda, tem `-dry-run`, reporta o liberado e é idempotente.
- [x] Limpeza automática ao concluir um pedido, com o atual intocável.
- [x] `.part`/`.ytdl` (resíduo de download interrompido) são removíveis.
- [x] Pedido que FALHA tem o resíduo limpo na hora (não espera a política).
- [x] Verificação de espaço antes da fase pesada: tenta limpar e, se ainda faltar, falha
      com "espaço insuficiente: N livres, precisa de ~M".
- [x] Testes cobrem política, whitelist, guarda de caminho, dry-run, idempotência,
      resíduo de falha e verificação de espaço (sem tocar disco real além de `t.TempDir`).
- [x] `go build ./...` e `go test ./...` verdes.

**Resultado da limpeza retroativa (2026-07-26):** `trabalho/` foi de **4,0 GB para 126 MB**
— **3,9 GB liberados** em 7 pedidos, retendo o mais recente. `finalizados/` (32 Shorts,
417 MB) e `resultados/tempos.csv` intactos.

## Como validar

```bash
go test ./...
go run ./cmd/limpar -dry-run      # mostra o que faria e quanto liberaria
go run ./cmd/limpar               # limpa, retendo o pedido mais recente
go run ./cmd/limpar -reter 3 -v   # retém 3 e lista os arquivos
```

## Registrado para depois: `finalizados/` cresce sem limite (por design)

Esta spec limpa `trabalho/` (o bruto). **`finalizados/` NÃO é tocado — de propósito**: são
os Shorts entregues, o produto. Mas ele cresce indefinidamente: **32 Shorts = 417 MB**
medidos hoje; a um culto por semana, ~**3 GB/ano**.

É bem mais lento que o bruto (que crescia ~571 MB por PEDIDO), então não é urgente. Mas
vale registrar a decisão que ficará: **depois de entregue, o Short é uma cópia** — o
operador baixa pela página e envia por WhatsApp, e o arquivo em `finalizados/` passa a ser
redundante. Opções para quando incomodar: (a) apagar Shorts de pedidos já entregues após um
prazo; (b) arquivar em outro volume; (c) manter só os N últimos pedidos, como no bruto.
Precisa de um sinal de "já entregue" que hoje não existe (o sistema não sabe se o operador
baixou). Não implementar antes de haver o sinal — apagar um Short que o operador ainda não
baixou seria perda real.

## Revisão de 2026-07-29 — o vídeo virou cache, e a política dele muda

### O que estava contraditório

Esta spec apagava `trabalho/<id>/video.mp4` dos pedidos anteriores, mantendo o do último. Com
o **cache por vídeo** (spec-05 v3), o arquivo deixou de pertencer a um pedido: ele é do
**culto**, mora em `videos/<idDoVídeo>/` e serve qualquer janela, de qualquer pedido, agora ou
na semana que vem. A política antiga, aplicada ao cache, apagaria o vídeo logo depois de
baixá-lo — o oposto do objetivo.

### Duas políticas, porque agora são dois níveis

| artefato | onde | política |
|---|---|---|
| vídeo, legenda, transcrição do culto | `videos/<idDoVídeo>/` | **prazo em dias + teto de tamanho**, avaliados juntos |
| candidatos, ajustes, `pedido.json` | `trabalho/<idDoPedido>/` | **contagem de pedidos** (a política atual, inalterada) |
| Shorts | `finalizados/` | intocado, como já era |

**Valores padrão (decisão do dono): 30 dias e 50 GB** (~90 vídeos de 570 MB). O disco tem
~516 GB livres; o teto é conservador de propósito.

### Por que prazo E teto, e não um só

- **Só prazo** não protege o disco: uma semana movimentada (vários cultos, testes, refazer com
  outra janela) enche o disco **antes** de qualquer arquivo completar 30 dias. Foi o ponto do
  dono, e é o mesmo raciocínio que levou esta spec a rejeitar prazo puro em 2026-07 — só que
  agora a resposta não é trocar prazo por contagem, é somar o teto.
- **Só teto** não limpa o que ninguém mais vai usar: um culto de três meses atrás ficaria em
  disco enquanto houver espaço, sem servir a nada.

Ordem de avaliação:

1. remove os vídeos cujo **último uso** é mais antigo que o prazo;
2. se o cache **ainda** passa do teto, remove do mais antigo para o mais novo até caber;
3. o vídeo do pedido **em curso** é intocável — mesma invariante estrutural de hoje.

### Idade pelo último USO, não pelo download

`video.json` carrega `usado_em`, tocado a cada reaproveitamento. Motivo concreto: um culto
reprocessado toda semana (o operador refazendo com outra janela, ou gerando mais Shorts do
mesmo sermão) tem `baixado_em` antigo e uso recente. Com FIFO puro, a limpeza apagaria
justamente o vídeo mais útil, e o próximo pedido pagaria 35 s de download por nada.

### O que NÃO muda

- **Preservados sempre:** `candidatos.corrigido.json`, `transcricao.txt`,
  `revisao-teologica.json`, `pedido.json`, e tudo em `finalizados/` e `resultados/`.
- **O pedido que falha é limpo na hora** (resíduo de `.part`/`.ytdl`).
- **A guarda de caminho** (`caminhoSeguro`) e a precedência de preservados sobre removíveis.
- **A verificação prospectiva de espaço** antes da fase pesada — com uma mudança: ela só
  precisa exigir espaço **quando o download vai de fato acontecer**. Com acerto de cache não
  há nada a reservar, e pedir 2 GB livres para não baixar nada seria falhar sem motivo.

### Efeito colateral bom: `-reter` pode crescer

Com o vídeo fora de `trabalho/`, cada pedido passa a ocupar **KB** (texto e JSON). O `-reter 1`
existia porque cada pedido carregava ~571 MB. Proposta a confirmar na implementação: subir o
padrão para **20 pedidos** — mantém histórico de candidatos por semanas sem custo real de
disco. Não é decisão fechada; fica registrada aqui para não passar batido.

### Novas flags

`cmd/servidor` e `cmd/limpar`: `-videos <dir>` (padrão `videos`), `-video-dias` (padrão 30),
`-video-teto` (padrão 50GB). `cmd/limpar -dry-run` continua mostrando o que faria — e passa a
listar o cache separado dos pedidos, porque são políticas diferentes e misturar os números
esconderia qual delas liberou o quê.

## Fora de escopo / próximos passos

Pipeline completo. Melhorias futuras (legenda palavra-a-palavra, modelo externo
opcional, métricas) entram como specs novas quando/se necessárias.
