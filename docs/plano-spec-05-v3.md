# Redesenho da spec-05 — quatro telas + cache por vídeo

> **O que é este arquivo.** O RASCUNHO do redesenho, como foi apresentado ao dono em
> 2026-07-29, com as sete decisões em aberto no fim. Está aqui porque o raciocínio e as
> alternativas descartadas valem registro — mas **não é a fonte de verdade**.
>
> **A fonte de verdade é a `docs/specs/spec-05-servidor-web.md`, seção "v3"**, que já
> incorpora as decisões do dono e traz critérios de aceite e como validar. Onde os dois
> divergirem, vale a spec. Divergências conhecidas (decisões tomadas DEPOIS deste rascunho):
>
> | ponto | rascunho | decidido (vale na spec) |
> |---|---|---|
> | largura da folha | "~1180px, a confirmar" | **1180px** |
> | prazo/teto do cache | proposta de 14 dias / 20 GB | **30 dias / 50 GB** |
> | refazer a seleção | três opções em aberto | **só "buscar outros trechos"** (temperatura > 0); não existe refazer avulso |
> | F5 na revisão | três opções em aberto | **avisar (`beforeunload`) e aceitar a perda** |
>
> As outras três decisões do fim (retenção de artefatos de pedido, o que "apagar Short" apaga,
> migrar os vídeos que já estão em `trabalho/`) seguiram com a recomendação escrita aqui.

> Desenho para revisão. Nada implementado. As decisões que são suas estão isoladas no fim.

## Contexto

A tela atual é **uma só**, montada por fragmentos que o HTMX troca. Isso produziu três
problemas que a rodada de uso expôs:

1. **Aperto.** `.folha { max-width: 720px }` ([templates.html:40](internal/servidor/templates.html#L40))
   com a revisão em duas colunas dentro. É a reclamação principal do operador.
2. **Sem navegação.** Não existe voltar. O operador vê a etapa que o servidor mandou, e só.
3. **Etapas invisíveis.** Baixar legenda (3 s) e selecionar (~32 s) aparecem como um texto
   "Processando… <estado>", sem indicação de onde está nem de quanto falta.

E há um problema de armazenamento: cada pedido cria `trabalho/<id>/` e **rebaixa tudo**,
inclusive os ~570 MB de vídeo — mesmo quando é o mesmo culto de meia hora antes. A spec-06
hoje **apaga** o vídeo depois de gerar, o que é o oposto de um cache.

Resultado pretendido: quatro telas navegáveis no cliente, mais largas e mais espaçadas; e
download de vídeo uma vez por culto, não uma vez por pedido.

---

## Parte 1 — Quatro telas, navegação no cliente

### As telas e o indicador

```
┌─ 1 dados ──┐  ┌─ 2 processando ─┐  ┌─ 3 revisão ─┐  ┌─ 4 resultado ─┐
│ link       │  │ legenda    ✓ 3s │  │ (a tela de  │  │ short_01 ▶ ⬇ 🗑 │
│ início/fim │→ │ seleção  ▓▓▓ 32s│→ │  hoje, na   │→ │ short_02 ▶ ⬇ 🗑 │
│ [enviar]   │  │                 │  │  largura    │  │ short_03 ▶ ⬇ 🗑 │
└────────────┘  └─────────────────┘  │  toda)      │  └────────────────┘
                                     └─────────────┘
```

O indicador de etapa é uma faixa de quatro botões, sempre visível no topo. Estados:
`atual`, `alcançada` (clicável), `bloqueada` (desabilitada), `erro`.

> Cuidado de nome: já existe `.trilha` na revisão — é a régua de trechos (um quadradinho por
> candidato). O indicador novo é `.etapas` / `#etapas`. Nomes diferentes de propósito, para
> ninguém reaproveitar o CSS errado.

**As quatro telas ficam no DOM desde o primeiro carregamento.** O servidor manda a página
inteira uma vez; trocar de tela é `hidden` para lá e para cá. Zero requisição.

### Estado no cliente

Um objeto só, ao lado do `REV` que já existe (`REV` continua sendo o estado da revisão —
trechos, ajustes, decisões; não muda):

```js
var APP = {
  tela: 'dados',
  pedidoId: null,
  alcancadas: { dados: true, processando: false, revisao: false, resultado: false },
  entrada: { url: '', inicio: '', fim: '' },  // p/ reexibir a tela 1 sem pedir ao servidor
  shorts: [],
  sujo: false,   // há decisão de revisão não enviada
};
function irPara(tela) { /* só mexe em hidden + classes do indicador */ }
```

### Divisão de trabalho (requisito do dono)

| quem | o quê |
|---|---|
| **JS vanilla** | navegar entre telas, mostrar/esconder, indicador, player, ajuste de corte |
| **HTMX** | criar pedido, polling do progresso, ajustar corte, aprovar, gerar, apagar Short |

Nenhum botão de navegação carrega atributo `hx-*`. Isso é verificável mecanicamente no
template (ver "Como validar").

### O que "navegável" significa, por etapa

Dois verbos, e a diferença é a regra inteira: **ver** é livre; **refazer** destrói trabalho.

| ação | permitido | efeito |
|---|---|---|
| ver qualquer etapa já alcançada | sempre, sem confirmar | só mostra. Nada no servidor. `REV` intacto |
| ver `dados` durante a revisão | sim | campos preenchidos em modo leitura + botão "refazer com outra janela" |
| ver `processando` depois de pronto | sim | mostra os tempos que as etapas levaram (vira registro, não spinner) |
| ver `revisão` a partir do resultado | sim | decisões visíveis, congeladas |
| avançar para etapa não alcançada | **não** | botão desabilitado; quem libera é o servidor (revisão só com candidatos; resultado só com Shorts) |
| **refazer a seleção** | confirmação | descarta candidatos, aprovações e ajustes; roda a fase leve de novo |
| **refazer com outra janela** (novo início/fim) | confirmação | mesmo descarte; **novo pedido**, vídeo reaproveitado do cache |
| **gerar de novo** (a partir do resultado) | confirmação | re-renderiza; sobrescreve os Shorts daquele pedido |

A confirmação **nomeia o que se perde**: "isso descarta 4 aprovações e 2 ajustes de corte".
Um "tem certeza?" genérico não informa nada.

### F5: como a tela se recupera

Ao carregar, **uma** requisição (carregar não é navegar): `GET /pedido-atual` →
`204` se não há nada, ou o mesmo payload do `GET /pedidos/{id}` que já existe.

| status no servidor | tela ao abrir |
|---|---|
| nada em memória | `dados` |
| fase leve em curso | `processando`, polling religado |
| `aguardando-aprovacao` | `revisao`, reidratada do payload (o JSON dos trechos já vem lá hoje, `RevisaoDados`) |
| fase pesada em curso | `processando` (bloco da fase pesada) |
| `concluido` | `resultado`, com a lista de Shorts |
| `erro` | a tela da etapa que falhou, com a mensagem |

**O que o F5 NÃO recupera: as decisões da revisão em andamento** (aprovado/reprovado e
ajustes ainda não enviados). Elas vivem só no `REV`. Proposta: `beforeunload` avisando
quando `APP.sujo` é verdadeiro, e ponto — persistir decisão a cada clique transformaria a
revisão numa conversa constante com o servidor, que é o oposto do requisito. Fica registrado
como melhoria possível, não como dívida.

O `-retomar <id>` continua existindo e é o caminho para trazer de volta um pedido de outra
execução — a reidratação cobre só o pedido que o servidor tem em memória.

### Largura e respiro

- `.folha`: **720px → ~1180px** (número a confirmar por você). O rodapé fixo acompanha.
- Escala de espaçamento em variáveis (`--esp-1: 8px` … `--esp-5: 32px`), aplicada em vez dos
  valores soltos de hoje: padding dos cartões 20/22 → 28, `gap` das colunas 18 → 28.
- A revisão ganha a folga onde falta: faixa de frases `max-height` 420 → ~560px.
- `@media (max-width: 760px)` continua (uma coluna) — o operador às vezes abre no celular.

---

## Parte 2 — Cache por vídeo (mudança de armazenamento)

### Dois níveis, porque os artefatos têm naturezas diferentes

```
videos/<idDoVídeo>/          # imutável, reutilizável, PESADO (~570 MB)
  video.mp4
  video.json                 # {video_id, origem_ms, baixado_em, usado_em, bytes, titulo}
  legenda.srt
  legenda.info.json
  transcricao.txt            # do vídeo INTEIRO

trabalho/<idDoPedido>/       # depende da janela e das decisões; leve (KB)
  pedido.json                # {..., video_id}
  transcricao.txt            # RECORTADA à janela (derivada; o que a seleção lê)
  candidatos.corrigido.json

finalizados/<idDoPedido>/short_NN.mp4
```

**Por que a transcrição aparece nos dois:** hoje ela é recortada à janela na hora do download
([ytdlp.go:262](internal/download/ytdlp.go#L262)), com tempos absolutos. Cache tem de ser do
**vídeo inteiro** para servir qualquer janela; o recorte continua sendo por pedido, porque é
o que a seleção e o `cmd/auditar` leem. É texto: recortar de novo é barato e não vale
inventar caminho novo para os consumidores.

**O ID do vídeo NÃO nomeia a pasta do pedido.** Pedido segue `web-<timestamp>-<n>`. Duas
pregações na mesma transmissão, ou o operador refazendo com outra janela, compartilham o
vídeo e mantêm candidatos separados.

**Sinergia registrada:** como o download é do vídeo **inteiro** (decidido por medição: 7,3 s
contra 577 s), o cache serve qualquer janela **sem verificação de cobertura**. Se fosse por
trecho, cada acerto de cache exigiria checar se a janela pedida cabe no que está em disco.

### A origem do vídeo mora ao lado do vídeo (ligação com a spec-09)

Esta é a parte que já custou duas rodadas, então o desenho é explícito:

> **Cada arquivo de vídeo carrega a própria declaração de origem, ao lado dele.**
> `videos/<id>/video.json` descreve `videos/<id>/video.mp4`.
> `pedido.json.origem_ms` descreve `trabalho/<id>/video.mp4` (fluxo do `cmd/baixar` por
> janela, que continua existindo).

Um localizador único, que é o **único** lugar que resolve "qual arquivo e qual origem":

```go
// internal/videocache (novo)
func Localizar(videosDir, baseDir string, ped *pipeline.Pedido) (path string, origemMs int, err error)
```

Regra, em uma frase: **vídeo na pasta do pedido vence** (é o fluxo por janela, mais
específico); senão o cache; se nenhum dos dois, erro claro dizendo o que falta. Nunca
dedução. Quem chama: fase pesada do servidor, `cmd/render`. `internal/video` não muda — ele
já recebe a origem por parâmetro.

### Extração do ID do vídeo

Já existe e já cobre `/live/`: [videoid.go](internal/servidor/videoid.go) +
[videoid_test.go](internal/servidor/videoid_test.go). O que muda:

- **muda de lugar** (`internal/servidor` → `internal/download`, exportada como
  `download.VideoID`), porque agora o download também precisa dela;
- **passa a ser validada com rigor**, porque o valor deixa de ser só um parâmetro de iframe e
  passa a ser **nome de diretório**: `^[A-Za-z0-9_-]{11}$`. Sem isso, uma URL hostil escolhe
  onde escrevemos. É a mesma preocupação do `retencao.caminhoSeguro`;
- casos de teste: `watch?v=`, `youtu.be/`, `&t=`, `&list=`, `/live/<id>`,
  `/live/<id>?feature=share`, `/embed/`, `/shorts/`, `m.youtube.com`, host em maiúsculas,
  URL de outro site (rejeita), e entrada hostil tipo `../../etc` (rejeita).

### Fluxo com cache

```
POST /pedidos
 └─ extrai video_id da URL  →  grava em pedido.json
 └─ fase leve:  videos/<vid>/ existe e completo?
      sim → reusa legenda+transcrição (0 s), toca usado_em
      não → baixa legenda para o cache
    → recorta transcrição à janela em trabalho/<id>/  → seleção
 └─ (aprovação)
 └─ fase pesada: videos/<vid>/video.mp4 existe?
      sim → pula o download (economiza ~35 s e 570 MB de banda)
      não → baixa o vídeo inteiro para o cache, grava video.json
    → render lê (path, origem) do Localizar
```

---

## Parte 3 — Retenção (revisa a spec-06)

O que está contraditório hoje: a spec-06 apaga `video.mp4` depois de gerar. Com cache, isso
joga fora justamente o que o cache existe para guardar.

**Nova política, dois níveis:**

| artefato | política |
|---|---|
| `videos/<vid>/` | expira por **prazo em dias** E **teto de tamanho**, avaliados juntos |
| `trabalho/<id>/`, `finalizados/` | política atual (contagem de pedidos) — agora são KB |

Ordem de avaliação:
1. remove os vídeos com `usado_em` mais antigo que `-video-dias`;
2. se o cache **ainda** passa de `-video-teto`, remove do mais antigo para o mais novo até
   caber. Sem o teto, uma semana movimentada enche o disco antes de qualquer coisa expirar —
   foi o seu ponto;
3. o vídeo do pedido em curso é intocável (mesma invariante de hoje).

**Idade por último USO, não por download** (`usado_em`, tocado a cada reaproveitamento): um
culto reprocessado uma semana depois sobrevive. FIFO puro apagaria exatamente o vídeo que
está sendo usado toda semana.

A verificação prospectiva de espaço (`GarantirEspaco`, 2 GB de margem) continua — mas passa a
só exigir espaço **quando o download vai de fato acontecer**. Com acerto de cache, não há o
que reservar.

---

## Parte 4 — Tela de resultado

Um cartão por Short: `<video controls preload="metadata">` (arquivo local, não YouTube),
duração, tamanho, e três ações: **assistir** (embutido), **baixar**, **apagar** (confirmação
nomeando o arquivo).

`DELETE /finalizados/{id}/{arquivo}` reusa a **mesma** validação de nome do
`handleBaixarFinal` (que já tem teste: `TestBaixarFinalRecusaArquivoForaDaWhitelist`). Um
endpoint que apaga com travessia de caminho é muito pior que um que baixa.

### Compartilhar: a limitação, com honestidade

Não há caminho bom, e o desenho registra isso em vez de fingir:

- **integração com WhatsApp não existe** e é decisão registrada (BRD/spec-05: envio é manual);
- **Web Share API com arquivo** (`navigator.share({files})`) é instável fora do celular —
  no desktop, ou não expõe o alvo certo, ou falha silenciosamente;
- **um botão que abrisse o WhatsApp sem anexar o vídeo seria pior que não ter**: promete o
  fluxo e entrega meia ação, e o operador descobre isso no meio do envio.

O fluxo real, escrito na tela em uma linha: **baixar e enviar pelo WhatsApp Web.** Sem botão
falso.

---

## Arquivos que mudam

| arquivo | mudança |
|---|---|
| `internal/servidor/templates.html` | 4 seções no DOM; `#etapas`; CSS de largura/respiro; JS `APP` + `irPara`; tela de resultado com `<video>` |
| `internal/servidor/servidor.go` | `GET /pedido-atual`; `DELETE /finalizados/...`; fase leve/pesada consultam o cache |
| `internal/videocache/` (novo) | layout de `videos/<id>/`, contrato do `video.json`, `Localizar`, `Registrar`, `Tocar` (usado_em), `Expirar` |
| `internal/download/` | `VideoID` (vinda do servidor, com validação estrita); escreve legenda/vídeo no cache; segue devolvendo a origem |
| `internal/retencao/limpeza.go` | política nova do cache (dias + teto + LRU); a de pedido não muda |
| `cmd/servidor`, `cmd/render`, `cmd/limpar` | flags novas (`-videos`, `-video-dias`, `-video-teto`); render resolve o vídeo pelo `Localizar` |
| `docs/specs/spec-05` | reescrita (v3); `spec-06` emendada; `spec-09` ganha o link do `video.json` |
| `.gitignore` | `videos/` |

Não muda: `internal/video` (recebe origem por parâmetro), `cmd/auditar` (só lê texto),
`internal/harness`, `internal/validacao`.

---

## Como validar

**Navegação sem requisição** (mecânico, é o requisito central):
- teste de template: as quatro seções existem; nenhum elemento dentro de `#etapas` tem
  atributo `hx-`; `irPara` não chama `fetch`/`htmx.ajax`.
- teste de referência de JS (o padrão que já existe em
  [js_referencias_test.go](internal/servidor/js_referencias_test.go)) cobrindo os ids novos.

**Cache** (é onde o dinheiro está):
- dois pedidos, **janelas diferentes, mesmo vídeo**: um download só, dois
  `candidatos.corrigido.json` distintos, nenhum sobrescrito;
- acerto de cache pula o download: fase pesada sem chamar o baixador;
- ponta a ponta real, com `tempos.csv` antes/depois — o número a mostrar é a fase pesada
  caindo de ~35 s para ~0 s de download.

**Origem** (a classe de bug que custou duas rodadas):
- `cmd/render` sobre um pedido cujo vídeo está **no cache** renderiza a cena certa,
  reaproveitando o teste de conteúdo com fonte sintética
  ([origem_do_video_test.go](internal/video/origem_do_video_test.go));
- `Localizar` com vídeo nos dois lugares → vence o do pedido; sem nenhum → erro claro.

**ID do vídeo:** tabela com as formas reais, incluindo `/live/` e `&t=`, e rejeição de
entrada hostil (não vira nome de pasta).

**Retenção:** expira por dia; expira por teto mesmo dentro do prazo; não toca no vídeo em
uso; `usado_em` protege o culto reprocessado.

**F5:** com pedido em cada estado, abrir a página cai na tela certa; sem pedido, cai em
`dados`.

---

## Decisões que são suas

1. **Largura da folha.** Proponho **1180px** (hoje 720). 1180 dá ~560px por coluna na
   revisão; 1360 daria mais respiro mas começa a espalhar o olho em telas grandes.
2. **Prazo e teto do cache.** Proponho **14 dias** e **20 GB** (~35 vídeos de 570 MB). O
   disco tem 516 GB livres hoje, então o teto é conservador de propósito.
3. **Retenção dos artefatos de pedido.** Com o vídeo fora de `trabalho/`, cada pedido passa a
   ocupar KB. Vale subir o `-reter` de 1 para, digamos, 20 pedidos? (Mantém histórico de
   candidatos sem custo real.)
4. **"Refazer a seleção" varia a temperatura?** Com `HARNESS_TEMP=0` (padrão, por
   auditabilidade) refazer dá **o mesmo resultado** — o botão viraria uma espera de 32 s sem
   efeito. Opções: (a) refazer só faz sentido junto com "buscar outros trechos" (temperatura
   > 0, já registrado na spec-05); (b) refazer existe só para depois de trocar a janela;
   (c) as duas coisas, botões separados.
5. **F5 durante a revisão.** Confirmar que perder as decisões não enviadas (com aviso do
   navegador) é aceitável, ou se quer que cada aprovação já vá ao servidor.
6. **Apagar Short apaga o quê?** Só o arquivo, ou também sai do registro do pedido
   (`cortes.csv`, que é dado de pesquisa sobre o desvio da legenda)? Proponho: só o arquivo —
   o registro é medição, não inventário.
7. **Vídeos que já estão em `trabalho/`** (hoje há um de 820 MB): migrar para o cache na
   primeira execução, ou deixar expirar pela política antiga? Proponho migrar (mover é
   instantâneo, mesmo disco) para não jogar fora um download bom.
