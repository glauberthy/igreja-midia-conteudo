# CLAUDE.md — Pipeline de Shorts de Sermões

Este arquivo é lido automaticamente pelo Claude Code. Descreve o projeto, as regras
invioláveis, as decisões já fechadas e como trabalhar aqui. Leia por completo antes de agir.

## O que é este projeto

Pipeline que transforma a gravação de um culto (publicado no YouTube) em vídeos curtos
verticais (Shorts) para o canal da igreja. Um modelo de linguagem local seleciona os melhores
trechos da pregação; um humano (o operador/pastor) revisa e aprova numa interface web; o
sistema baixa, corta e renderiza.

O objetivo é **edificação e ensino**, não engajamento. BRD completo em
`docs/BRD_Pipeline_Shorts_Sermoes_v0_2.md`.

## Estado atual — o ciclo está FECHADO e em uso

Do link do YouTube à entrega dos Shorts, sem terminal, em ~90 s de máquina no primeiro
pedido de um culto e ~27 s quando o vídeo já está no cache (medido; ver `resultados/tempos.csv`).
O operador cola o link e a janela da pregação, acompanha o progresso, revisa os trechos num
player embutido, ajusta o corte se precisar, aprova, e baixa os arquivos.

Existe e funciona: download, **cache por vídeo**, seleção multifase, validação determinística,
interface web de revisão, ajuste manual do corte, corte, reenquadramento 9:16, logo, gradiente
de rodapé, auditoria, retenção/limpeza de disco, instrumentação de tempo.

**Não existe ainda:** confronto doutrinário (spec-14), alinhamento forçado de áudio
(Rota D), o redesenho da interface em **quatro telas** (spec-05 v3 Parte 1) e a **expiração
do cache por prazo/teto** (Parte 3 — hoje o cache só cresce).

**Suspenso temporariamente:** a queima de legenda no Short final (spec-12) — desligada por
flag, não removida. Volta quando a Rota D resolver o timestamp por palavra.

Pendências e histórico de decisões: `docs/pendencias-priorizadas.md` e
`docs/ideias-futuras.md`. Leia antes de propor mudança grande — muita coisa já foi medida,
decidida e registrada com o motivo.

## Stack

- **Go** (biblioteca padrão; dependência externa só com justificativa)
- **llama.cpp** (`llama-server`) servindo **Gemma 4 26B A4B** localmente na porta 8080
- **yt-dlp** para download (requer um runtime JS — **deno** — instalado; sem ele a extração
  é degradada e o yt-dlp avisa)
- **ffmpeg** para corte, reenquadramento, gradiente e logo
- **HTMX + JavaScript vanilla** no front (sem framework, sem build, sem npm). O JS vanilla
  cuida do player do YouTube (IFrame API) e da navegação local. **CSS na mão, sem Tailwind.**

## Estrutura

```
cmd/servidor/      # interface web do operador (porta 7799) — o caminho principal
cmd/baixar/        # download por janela [inicio, fim] (CLI)
cmd/selecionar/    # seleção (harness) em produção
cmd/harness/       # seleção fase a fase, para auditoria (-ate N)
cmd/render/        # corte + 9:16 + logo + gradiente (+ legenda, hoje suspensa)
cmd/auditar/       # auditoria determinística dos candidatos (sem LLM)
cmd/limpar/        # retenção e limpeza de disco
cmd/srtclean/      # limpa legenda .srt -> transcrição [HH:MM:SS] texto
cmd/validar/       # validação determinística fora do fluxo (diagnóstico) — ver nota abaixo
internal/harness/  # Fases 1-5, Frasear, TranscricaoLinear, retry, cliente do modelo
internal/video/    # ffmpeg, filtros, legenda; recebe (arquivo, origem) de fora
internal/videocache/ # cache por vídeo: videos/<id>/, video.json e o RESOLVEDOR (Localizar)
internal/download/ # yt-dlp; VideoID (extrai e valida o id da URL)
internal/servidor/ # fase leve, fase pesada, revisão, retomada, migração p/ o cache
internal/pipeline/ # Pedido (estado, video_id, origem_ms, proveniência do recorte)
internal/transcricao/ # srtclean: SRT -> "[HH:MM:SS] texto", com recorte de janela
internal/validacao/# Candidato
internal/retencao/ # política de limpeza
internal/processo/ # execução com grupo de processos e kill seguro
prompts/           # prompts das fases do modelo
assets/            # logo (PNG) e fontes (Google Sans Flex)
docs/specs/        # specs 01-16 (uma por incremento)
docs/mockups/      # referências visuais de UI (HTML interativo)
docs/medicoes/     # medições, scripts para repeti-las e a ferramenta de nitidez
videos/<idVídeo>/  # CACHE do culto: vídeo, legenda, transcrição íntegra (NÃO versionar)
trabalho/<idPedido>/ # artefatos do pedido: candidatos, transcrição recortada (NÃO versionar)
finalizados/<id>/  # Shorts entregues (NÃO versionar)
resultados/        # tempos.csv, cortes.csv, rodadas.md (NÃO versionar)
```

## Comandos

```bash
# subir o modelo (obrigatório para a seleção)
bash ~/start-gemma-otim.sh

# interface web (caminho principal do operador)
go run ./cmd/servidor                      # http://localhost:7799
go run ./cmd/servidor -retomar <idPedido>  # reusa artefatos em disco, pula seleção

# CLI (fluxo alternativo, por janela)
go run ./cmd/baixar -url "<url>" -inicio HH:MM:SS -fim HH:MM:SS -id <id>
go run ./cmd/selecionar -transc trabalho/<id>/transcricao.txt \
    -out trabalho/<id>/candidatos.corrigido.json -prompt-dir prompts/
go run ./cmd/render -id <id>

# auditoria e diagnóstico
go run ./cmd/harness -transc trabalho/<id>/transcricao.txt -ate 5   # funil fase a fase
go run ./cmd/auditar -id <id> [-texto] [-criterios]
go run ./cmd/auditar -todos
go run ./cmd/validar -json trabalho/<id>/candidatos.corrigido.json \
    -transc trabalho/<id>/transcricao.txt          # confere um par (ver a nota sobre validar)

# limpeza de disco (DUAS políticas: pedidos por contagem, cache por prazo+teto)
go run ./cmd/limpar -dry-run
go run ./cmd/limpar -reter 1                     # também aceita -exceto <id> e -v
go run ./cmd/limpar -video-dias 30 -video-teto 50 # expiração do cache: 30 dias sem uso, 50 GB

# verificação
go build ./... && go vet ./... && go test -race ./...
```

### Nota sobre o `cmd/validar` (verificado no código, não é legado morto)

Ele tem **dois modos**, e só um continua valendo:

- **`-json <arquivo> -transc <arquivo>`** — confere um par candidatos/transcrição do layout
  ATUAL. É o modo usado pelo runbook para conferir um `candidatos.corrigido.json` já gerado, e
  funciona. Com `-corrigir`, grava a versão corrigida.
- **`-de N -ate M -dir resultados`** — modo lote do SPIKE: espera
  `resultados/candidatos_N.json` + `transcricao_N.txt`, layout que o fluxo atual não produz
  mais. Continua compilando e testado, mas não aponta para nada que o pipeline gere hoje.

Ou seja: **ferramenta de diagnóstico com um modo obsoleto**, não "a versão standalone da
validação do fluxo". Quem valida no fluxo é a Fase 5.

## Regras invioláveis

Não se negociam. Se uma tarefa parecer exigir quebrá-las, **pare e pergunte**.

1. **Teologia acima de engajamento.** A seleção prioriza fidelidade e ensino. Um trecho que
   engaja mas distorce a mensagem é um trecho ruim.

2. **Nunca alterar as palavras do pregador.** A limpeza de transcrição remove apenas
   marcação e a repetição da legenda rolling — nunca fala. **Apagar palavra é tão grave
   quanto alterá-la, e é silencioso.** (BRD RN-013)

3. **Validação determinística é obrigatória.** Nenhum candidato do modelo chega ao humano
   sem passar pela validação. No fluxo atual quem valida é a **Fase 5 do harness**
   (`internal/harness/fase5.go`), sobre `internal/validacao`. O modelo erra timestamp, score
   e às vezes inventa o hook. O `cmd/validar` NÃO está nesse caminho — é diagnóstico (nota
   abaixo).

4. **Segredos nunca no código nem na saída.** Nada de chave em arquivo versionado, log ou
   JSON. (BRD RN-038)

5. **LLM só para julgamento; código para o determinístico.** Escolher trecho e avaliar
   fidelidade = modelo. Timestamp, soma de score, duração, parsing, faixa de duração =
   código. Nunca peça ao modelo para fazer conta ou copiar número com precisão.

6. **Conteúdo sensível vai para revisão humana.** Trechos com suspeita de fidelidade são
   **marcados** (`requer_revisao_reforcada`), **nunca descartados** pelo modelo e nunca
   publicados automaticamente. O modelo não tem poder de veto sobre fidelidade — ele
   levanta suspeita; o humano decide. (spec-11)

7. **O agente NÃO opera canais de comunicação da igreja.** Não envia por WhatsApp, não manda
   e-mail, não publica no canal do YouTube, não posta em rede social — **nem para testar**.
   Vale para qualquer canal externo, presente ou futuro, e independe de haver credencial à
   mão. O que sai por esses canais sai **em nome da igreja, para pessoas reais, e não tem
   desfazer**: um Short errado publicado é dano pastoral, não bug. Isso reforça a decisão de
   produto que já está no BRD e na spec-05 — **o envio é manual**, feito pelo operador.
   Teste que envolva canal externo é **do dono**: o agente prepara o arquivo, diz exatamente o
   que conferir, e **aguarda**. E nunca afirma como verificado o que só o dono pode verificar.

   **A mesma regra vale para a máquina do dono: verificação visual que exija navegador é dele.**
   O agente não abre janela de navegador nesta máquina. Prepara o comando, diz o que a tela deve
   mostrar, e aguarda. Motivo concreto: para confirmar o primeiro quadro do player eu abri o
   Chrome no display do dono e capturei a **tela inteira** — a imagem pegou abas pessoais
   (WhatsApp, Gmail). Não vazou, e é exatamente o tipo de acesso que não pode virar rotina.
   Se um dia a renderização automatizada for necessária: **display isolado** (Xvfb próprio),
   captura **só da janela** (nunca `x11grab` da tela toda), e **declarado antes, não depois**.

## Decisões fechadas — não reabrir sem motivo novo

Todas foram tomadas com medição. O registro completo, com os números, está em
`docs/pendencias-priorizadas.md`.

- **Temperatura do modelo = 0** (`HARNESS_TEMP`), por **auditabilidade**: com temperatura
  fixa, "mesma entrada, mesma saída" vira ferramenta de depuração — se mudou, alguém mexeu
  em algo. Não é reprodutibilidade exata (há resíduo de não-determinismo de hardware).
- **Download: vídeo INTEIRO com `--concurrent-fragments 8`.** Medido: 7,3 s contra 577 s da
  janela contígua com `--download-sections`. O fator dominante é paralelismo, não bytes.
- **A origem do corte é declarada, nunca deduzida** (`origem_ms`, ponteiro; ausente = falha
  alta). Quem **escreve** o vídeo declara onde ele começa, **devolvendo** o valor — não
  escrevendo no Pedido, porque mutação em cópia se perde em silêncio. (spec-09)
- **Um resolvedor único de fonte: `videocache.Localizar`** devolve ARQUIVO e ORIGEM juntos,
  com precedência explícita (vídeo do pedido vence o do cache). O `internal/video` **não tem
  acesso** à origem — não é proibido deduzir, é impossível. Há uma varredura de AST que falha
  se algum consumidor voltar a ler a origem por fora.
- **O cache guarda o culto, não o pedido:** `videos/<idVídeo>/` (vídeo, legenda, transcrição
  íntegra) contra `trabalho/<idPedido>/` (candidatos, transcrição recortada). O id do vídeo
  **nunca** nomeia a pasta do pedido — duas pregações na mesma transmissão sobrescreveriam os
  candidatos uma da outra.
- **`videocache.Registrar` recusa origem != 0.** No cache só entra vídeo INTEIRO: um arquivo
  de janela ali teria `origem_ms: 0` mentindo sobre o conteúdo, **em disco**, envenenando todo
  pedido futuro que reusasse o culto.
- **A transcrição recortada é artefato DERIVADO**, com proveniência declarada no pedido
  (`recorte`), nunca editada, e há teste que regenera e compara byte a byte.
- **Ordem: revisão cronológica, render por score.** Dois contextos, duas ordens, de
  propósito — ordenar a revisão por score empurraria os trechos marcados para o fim da fila.
- **Saída em 1080x1920.** Emitir 720 transferiria a ampliação para o player, com um segundo
  reamostramento pior (rosto perde 26-37%, logo 64%).
- **Escalador lanczos, encode `medium/crf20`.** O preset domina o CRF (veryfast→medium rende
  +3,3% de laplaciano; crf20→18, +1,5%, dentro do ruído). O `crf18` vigorou até 2026-07-29,
  quando chegou o outro lado da conta: nos 4 Shorts reais ele custava **+20,3% de disco**
  (167,7 MB contra 133,7 MB) para uma diferença de imagem de **~45 dB de PSNR** — abaixo do
  limiar de percepção. Em conteúdo que já é ampliação de 720p macio, bit extra preserva maciez,
  não detalhe.
- **Gradiente de rodapé: altura 520, opacidade 0,60.**
- **Margem de recuo do corte = 0.** A causa do corte curto é o timestamp da legenda, não
  vazamento; a margem só agravava.

## Como trabalhar aqui

- **Use plan mode em tarefas grandes.** Explore e proponha o plano antes de editar; só
  implemente depois de aprovado.
- Siga a spec ativa em `docs/specs/`. Uma spec por vez. Trabalho incremental: fatias
  pequenas e verificáveis.
- **Investigue e reporte ANTES de corrigir**, quando o dono pedir. Diagnóstico independente
  vale mais que correção rápida — várias vezes neste projeto a hipótese estava errada e
  corrigir direto teria quebrado o que funcionava.
- **Meça antes de otimizar.** Já otimizamos o alvo errado mais de uma vez (o download era
  13% do tempo enquanto a atenção estava nele; o `geq` do gradiente parecia caro e custava
  0,15 s).
- Um assunto, um commit.
- Não reabra decisões registradas em `docs/aprendizados-do-spike.md` nem na seção acima.

## Simplicidade — código profissional, não engenharia de foguete

O sistema resolve um problema concreto de uma igreja. Deve ser **sólido onde a falha é cara** e
**simples em todo o resto**.

- **A solução mais simples que resolve o problema real.** Se a versão direta funciona, ela é a
  certa. Elegância não é critério; clareza é.
- **Defesa proporcional ao custo da falha.** Apagar arquivo em silêncio, gerar o vídeo da cena
  errada, envenenar o cache — falhas caras e silenciosas, guarda pesada justificada. Um campo
  novo numa struct, um valor fora de faixa que estoura na hora — baratos e visíveis, não
  merecem maquinaria.
- **Prefira tornar o erro impossível a vigiá-lo.** Remover o acesso vale mais que um teste que
  fiscaliza o acesso, e costuma dar MENOS código. (Foi o que se fez com o resolvedor de origem:
  o `internal/video` deixou de ter de onde deduzir.)
- **Toda guarda tem custo de manutenção.** Verificação que dispara falso com frequência vira
  ruído, e ruído ensina a ignorar. Se uma guarda exige atualizar uma lista de permissão a cada
  mudança legítima, ela provavelmente devia ser outra coisa.
- **Não abstraia para futuro hipotético.** Três ocorrências justificam uma abstração; uma não.
  Pacote novo pede justificativa; função nova, não.
- **Menos camadas, menos indireção.** Antes de criar um pacote, pergunte se uma função no
  pacote existente resolve.

O que já existe de maquinaria pesada foi ganho por bugs reais e caros — **não remova**. Mas não
generalize: o próximo problema provavelmente não precisa do mesmo peso.

**Ao propor guarda, teste de varredura ou pacote novo, escreva uma linha dizendo por que a
versão simples não bastava.** Se não houver essa linha, faça a simples.

> Caso concreto que já passou do ponto: a varredura por AST que impede leitura da origem fora
> do resolvedor (`internal/videocache/resolvedor_unico_test.go`), com lista de permissão e um
> segundo teste vigiando a lista. Protege algo real e caro, mas cobra manutenção a cada
> consumidor legítimo novo. **Não replicar o padrão.** Se começar a dar falso positivo, a saída
> é tornar o erro impossível — como se fez removendo o acesso à origem do `internal/video` —, e
> não afinar a lista.

## Testes — o que este projeto aprendeu do jeito difícil

**Cinco testes deste projeto passaram com o bug presente.** O padrão é sempre o mesmo:
medir o sintoma conveniente em vez da propriedade real. Por isso:

- **Verifique todo teste de invariante por MUTAÇÃO:** reintroduza o bug e confirme que o
  teste falha, e falha *nomeando a causa*. Teste que nunca falhou não é teste, é decoração.
- **Se uma asserção óbvia não distingue nada, mantenha-a no teste com um comentário
  dizendo isso** — vira aviso permanente para o próximo leitor. Apagar perde a lição.
- **Duração não prova conteúdo.** Três vezes neste projeto uma duração correta escondeu a
  cena errada. Para corte, teste a CENA (ex.: fonte sintética que codifica o instante no
  próprio pixel, render real, comparação de pixel).
- **Verificar que uma constante existe não prova que ela é usada.** Três vezes um valor
  existiu num lugar e o caminho real usou outro. Prove no caminho que o operador usa.
- **Descrição de mudança de formato precisa ser conferida CONTRA O CÓDIGO, não contra a
  intenção.** Caso real: uma mensagem de commit afirmou "coluna no fim do CSV", o código a
  inseriu no meio, e o trabalho seguinte (a migração do cabeçalho) confiou na afirmação e
  nasceu errado. Não é comentário que envelheceu — era **falso na origem**, e por isso mais
  perigoso: descrição de commit não tem teste. Ao descrever formato (ordem de coluna, nome de
  campo, layout de arquivo), leia o diff antes de escrever a frase.
- **Um teste de EFEITO não prova qual caminho agiu.** A expiração do cache é chamada em dois
  lugares (antes do download e depois de concluir). Tirar uma das duas chamadas não fazia teste
  nenhum falhar: o teste de ponta a ponta continuava verde porque o outro caminho limpava o mesmo
  arquivo. Quem achou o furo foi a **mutação**, não a leitura do teste. Política com mais de um
  ponto de entrada precisa de um teste que isole cada um.
- **Ao demonstrar operação destrutiva, use dado SINTÉTICO — nunca produção "com proteção".**
  Caso real: para mostrar a expiração do cache eu copiei `videos/` com `cp -rl` (hard link),
  achando que o link protegia o original. Protege contra **apagar** (remover um link não remove os
  dados) e não protege contra **escrever**: o `write` atravessa o inode compartilhado. E o arquivo
  que eu reescrevi foi justamente o pequeno que **governa a exclusão** (`video.json`, o
  `usado_em`), então o cache real ficou marcado como velho e a próxima limpeza apagaria 821 MB de
  verdade. **Defesa com cobertura assimétrica é pior que nenhuma**, porque dá confiança onde não
  há. Cache de teste com `truncate -s 25M` custa um segundo e não tem esse risco.
- **Duas listas para o mesmo dado divergem.** O `tempos.csv` quebrou duas vezes com cabeçalho
  numa lista e valores em outra. A correção não foi mais uma migração: foi **uma fonte só**
  (`colunasTempos`, nome e valor na mesma entrada). Migração conserta o sintoma; fonte única
  torna a próxima quebra impossível.

## Como reportar

**Prova de entrega (obrigatória).** Para **cada** item entregue:

1. **Onde está** — arquivo e linha.
2. **Como eu confirmo sozinho** — um comando que o dono roda no terminal dele, cujo
   resultado seria **diferente** se a mudança não existisse. Não vale comando que passa de
   qualquer jeito.
3. **O que esperar** — a saída esperada, para comparação.

Comece o relatório pela saída de `git show --stat HEAD` (ou o intervalo de commits da
tarefa), para o dono ver de imediato quais arquivos foram tocados.

**Não basta afirmar que fez, nem que os testes passaram.** Neste projeto, cinco testes
passaram com o bug presente, uma substituição de bloco falhou em silêncio e engoliu quatro
funções, e um `echo` declarou a suíte verde enquanto ela falhava.

**Item sem verificação observável** (comportamento no navegador, julgamento visual,
dependência de rede) — diga explicitamente e classifique como **"feito, não verificado"**,
em vez de listá-lo junto com o que foi provado. Declarar honestamente o que não foi
verificado vale mais que uma lista uniforme de itens "prontos".

**Item que você não conseguiu fazer** — marque como bloqueado, com o motivo. Silêncio faz o
dono descobrir a ausência três rodadas depois.