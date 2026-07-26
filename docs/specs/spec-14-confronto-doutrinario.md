# Spec 14 — Confronto doutrinário dos trechos marcados

## Objetivo

Transformar a marcação genérica de fidelidade (`requer_revisao_reforcada` com um motivo
vago) num **veredito focado e citável** para os poucos trechos que a Fase 4 já suspeitou.
Uma fase nova roda SÓ nesses trechos e confronta cada um com a Declaração Doutrinária —
mas **com o contexto ao redor**, que é o que a Fase 4 não tem. Classifica em quatro:
**fiel**, **ambíguo isolado** (o sermão resolve → o problema é o CORTE, não a doutrina),
**desalinhamento** (com o ponto citado) ou **provável erro de transcrição** (ASR). O
resultado **enriquece** a marcação que já vai ao operador — nunca descarta.

## Contexto

Hoje (Fase 4), o modelo pontua `context_fidelity` (0–30) com a Declaração no prompt; se a
nota fica abaixo de 18 (ou as duas avaliações divergem), o código marca
`requer_revisao_reforcada` com um motivo genérico ("possível problema de fidelidade —
revisar"). Isso já chega marcado à interface web (spec-05/spec-11). O que falta é
**dizer ao pastor o quê**: um número baixo não explica se o trecho contradiz a doutrina,
se é só ruído da legenda automática, ou se está tudo bem e o limiar disparou à toa.

**Relação com o `cmd/auditar` (spec-16):** o `cmd/auditar` é 100% determinístico (sem
LLM) — ele **exibe** o indicador que a Fase 4 já produziu e checa a mecânica do corte.
Ele **não** faz um julgamento teológico novo. Portanto esta spec é uma **capacidade
nova**, não uma duplicação: o harness **gera** o veredito; o `cmd/auditar` pode passar a
**exibir** (read-only) o veredito persistido, sem perder sua natureza determinística.

Origem da ideia: conversa de 2026-07-24, registrada em `docs/ideias-futuras.md` ("Confronto
doutrinário dos trechos marcados").

## Escopo

Dentro:
- Uma **nova fase no harness** (**Fase 6 — Confronto doutrinário**), que roda **só nos
  trechos com `requer_revisao_reforcada = true`** (tipicamente 0–2 por sermão).
- Por trecho marcado: uma chamada ao modelo com **o trecho DENTRO do seu contexto**
  (`contexto-antes → TRECHO → contexto-depois`) + a Declaração Doutrinária, pedindo uma
  classificação estruturada (contrato abaixo).
- **Enriquecer** o `motivo_revisao` do candidato com o veredito (específico e citável) e
  **persistir** os sinalizados num arquivo auditável por pedido.
- Guarda anti-falso-positivo embutida no prompt (obrigatória — ver Decisões).

### Por que CONTEXTO é o ponto central desta spec (correção de desenho)

A primeira versão desta spec mandava ao modelo **apenas o trecho + a Declaração**. Isso era
um erro: **a Fase 4 já julga o trecho isolado**. Repetir o mesmo material com outro prompt é
*a mesma lente apontada de novo* — o modelo continua sem saber se o pregador esclareceu a
questão vinte segundos depois. É exatamente a raiz dos falsos positivos que observamos (o
caso do João 17).

O que muda o jogo é dar o **entorno**: `±60–90 s` de transcrição antes e depois do trecho
(algumas centenas de tokens por lado — barato, e roda em 0–2 trechos por sermão). Com o
entorno, o confronto passa a distinguir duas coisas que hoje chegam como **o mesmo ⚠ vago**:

- o trecho é ambíguo sozinho, **mas o sermão resolve** → o problema é **o CORTE**, não a
  doutrina. O pregador está certo; a janela ficou curta. Ação: **estender o trecho**.
- o trecho é problemático **mesmo com o contexto** → preocupação **doutrinária** real.

Essa é a distinção "conteúdo ruim" × "corte ruim", que o sistema hoje não faz.

Fora:
- **Não** roda nos trechos não marcados (custo e ruído desnecessários).
- **Não** descarta nem re-pontua nada — só enriquece a marcação (espírito da spec-11).
- **Não** vira parte do `cmd/auditar` (que continua determinístico); no máximo o auditar
  ganha uma exibição read-only do veredito persistido — item opcional, fora desta spec.
- **Não** certifica doutrina: o pastor segue como certificador final (regra nº 6).

## Decisões já tomadas (não reabrir)

- **Só nos trechos marcados.** A entrada é o conjunto `requer_revisao_reforcada = true`.
  Se não houver nenhum, a fase é no-op (custo zero).
- **Posição no pipeline: após a Fase 5** — por isso o nome **Fase 6**. Roda sobre os
  candidatos finais que sobreviveram à validação determinística, evitando gastar chamada em
  trecho que a Fase 5 descartaria por motivo mecânico. (Nome antigo do rascunho, "Fase
  4.5", foi abandonado: induzia a procurar a etapa entre a Fase 4 e a 5, onde ela não está.)
- **Nunca limpa a marcação.** Mesmo com veredito `fiel`, o trecho continua
  `requer_revisao_reforcada = true` — o confronto é uma segunda lente, não um override; o
  humano decide. O que muda é o `motivo_revisao`, que fica específico.
- **A Declaração já está no cache de prompt da Fase 4** (mesmo prefixo de sistema), então
  o confronto é barato.
- **Entra com CONTEXTO (±60–90 s de cada lado), não com o trecho isolado** — é o que
  diferencia esta fase da Fase 4 (ver seção acima). O contexto sai da mesma transcrição
  desduplicada (`harness.Frasear`/`TranscricaoLinear`), recortado por tempo em torno de
  `[start, end]`; nas bordas do sermão, o que houver.
- **O veredito é sobre o TRECHO, não sobre o sermão.** O contexto serve SÓ para desambiguar
  a intenção do pregador. Um trecho "resolvido no contexto" CONTINUA sendo problema de
  publicação — só de outro tipo (corte, não conteúdo), porque é o trecho que vai ao ar
  isolado. Instrução explícita no prompt (ver guarda).
- **Guarda anti-falso-positivo é obrigatória** (ver seção própria).
- **É modelo confrontando modelo** — reduz erro com uma lente nova, NÃO adiciona uma fonte
  de verdade. Registrar essa ressalva no output e na doc.

## Contrato

Entrada por trecho marcado (o contexto é o que torna esta fase diferente da Fase 4):

```
## CONTEXTO ANTES (não julgar; só para entender a intenção)
<±60–90 s de transcrição antes do start>

## TRECHO (é ISTO que vai ao ar isolado — o veredito é sobre ele)
<texto do trecho>

## CONTEXTO DEPOIS (não julgar; só para entender a intenção)
<±60–90 s de transcrição depois do end>
```

Saída (JSON, validada com retry — spec-08):

```
{
  "classe": "fiel" | "ambiguo_isolado" | "desalinhamento" | "provavel_erro_transcricao",
  "ponto_citado": "<ponto/tema da Declaração, obrigatório se desalinhamento; senão vazio>",
  "onde_resolve": {                    // obrigatório se ambiguo_isolado; senão ausente
    "frase": "<a frase do CONTEXTO que desambigua — copiada literalmente>",
    "lado":  "antes" | "depois"
  },
  "motivo": "<explicação curta e legível para o pastor>"
}
```

**Por que `onde_resolve` é obrigatório em `ambiguo_isolado`:** sem ele, o operador recebe
"estenda" e precisa **caçar até onde** — e a classe nova vira só marginalmente melhor que o
⚠ vago que ela veio substituir. O modelo **já leu o contexto**; devolver a frase que resolve
não custa nada e transforma a mensagem em algo acionável: *"estenda até 'X'"*. O `lado` diz
se é para estender o início (antes) ou o fim (depois).

As quatro classes, e o que cada uma significa para a AÇÃO do operador:

| classe | significado | ação natural |
|---|---|---|
| `fiel` | ok isolado e no contexto | aprovar |
| **`ambiguo_isolado`** | **o sermão resolve, o fragmento sozinho não** → problema de **CORTE** | **estender o trecho** |
| `desalinhamento` | problemático **mesmo com o contexto** → doutrina | revisar com atenção (ponto citado) |
| `provavel_erro_transcricao` | ruído de ASR, não doutrina | conferir o texto/legenda |

Efeito no candidato (`validacao.Candidato`), mantendo `requer_revisao_reforcada = true`.
O candidato passa a carregar **a classe do confronto** (`classe_revisao`) além do
`motivo_revisao` enriquecido — é a classe que dirige a EXIBIÇÃO (quatro tratamentos: um por
classe; ver abaixo e a spec-05). Sem confronto (não implementado ainda, ou trecho não
marcado), a classe fica vazia e a exibição cai no comportamento atual (⚠ genérico).
- `desalinhamento` → `motivo_revisao = "desalinhamento doutrinário: <ponto_citado> — <motivo>"`.
- `ambiguo_isolado` → depende de a extensão CABER na faixa de 30–58 s (cálculo do código,
  ver seção adiante): cabe → `"o corte ficou curto: estenda até '<frase>' (+Ns, total ~Ms)"`;
  não cabe → `"ambíguo isolado, mas não dá para consertar por extensão (ficaria ~Ms): reprove ou aceite ciente"`.
- `provavel_erro_transcricao` → `motivo_revisao = "provável erro de transcrição (não doutrina): <motivo>"`.
- `fiel` → `motivo_revisao = "conferido: sem problema doutrinário aparente — <motivo>"`.

Persistência (auditável): `trabalho/<id>/revisao-teologica.json` com a lista
`{start, end, hook, classe, ponto_citado, motivo}` dos trechos confrontados. Opcional: um
`.md` legível agregando os sinalizados.

Assinatura sugerida (a confirmar na implementação) — recebe a transcrição para recortar o
contexto de cada trecho:

```
func Fase6Confronto(ctx, modelo ModeloLLM, declaracao, transcricao string,
    finais []validacao.Candidato) ([]validacao.Candidato, []VeredictoConfronto)
```

### Estender CABE na faixa? — isso é CÓDIGO, não modelo

Dizer "estenda" sem verificar se a extensão **cabe** produz um conselho impossível. Se a
frase que resolve está 40 s adiante, estender jogaria o Short para ~70 s — **fora da faixa
de 30–58 s** (spec-07/Fase 3). O veredito honesto aí não é "estenda", é "não dá para
consertar por extensão".

Divisão de trabalho (a mesma que organiza o projeto — regra nº 5):
- **o modelo diz ONDE resolve** (`onde_resolve.frase` + `lado`) — é julgamento;
- **o código calcula SE cabe** — é aritmética: localiza a frase no `Frasear` (mesmo
  casamento do `AcharAncora`), pega o timestamp dela e compara com o `start`/`end` atual:
  `duracao_estendida = (fim_da_frase_que_resolve) - start` (ou `end - inicio_da_frase`, se
  `lado = antes`). Cabe se ficar em **30–58 s**.

Os dois desfechos, na mensagem ao operador:

| cabe na faixa? | `motivo_revisao` (mensagem) |
|---|---|
| **sim** | "o corte ficou curto: estenda até *'<frase>'* (+Ns, total ~Ms) — o sermão esclarece ali" |
| **não** | "ambíguo isolado, mas **não dá para consertar por extensão** (ficaria ~Ms, acima de 58 s): reprove, ou aceite ciente da ambiguidade" |

Sem esse cálculo, metade dos `ambiguo_isolado` viraria orientação inexequível — e o operador
aprenderia a ignorar a classe, exatamente o que a spec quer evitar.

Quando o veredito é `ambiguo_isolado`, a ação natural é **estender o trecho** — que é
exatamente o **ajuste fino do corte pelo operador**, já registrado como v2 da spec-05. As
duas frentes se encaixam: **o confronto diz "o corte ficou curto"; o ajuste manual permite
consertar.** Sem o ajuste, o operador só pode reprovar um trecho cujo conteúdo é bom — o
que é desperdício. Vale implementar as duas em sequência (ou pelo menos ter o ajuste no
horizonte quando esta classe começar a aparecer).

## Exibição por nível de alerta (contra fadiga de alerta)

Manter a flag em todos os marcados evita desmarcar algo silenciosamente — mas se **tudo**
continua com ⚠ forte mesmo depois de conferido, a marcação nunca diminui e o operador
entra em **fadiga de alerta**: vê ⚠ em tudo, aprende que quase nunca é nada, e passa a
ignorar inclusive quando importa. Um alerta que sempre aparece deixa de ser alerta.

Solução: a flag permanece (nada some), mas a **classe do confronto** define **um tratamento
de exibição por classe** (quatro no total) — na trilha e no card da tela de revisão
(spec-05), em três NÍVEIS de intensidade (alto / médio-com-ação / baixo):

| classe | nível | como aparece |
|---|---|---|
| `desalinhamento` | **alto** | ⚠ âmbar, destaque forte, **com o ponto citado** da Declaração |
| `ambiguo_isolado` | **médio, com AÇÃO** | ✂ "o corte ficou curto — o sermão esclarece; considere estender". Não é alerta de doutrina: é convite a ajustar o trecho |
| `provavel_erro_transcricao` | baixo | ℹ neutro/quieto (cinza): "conferido: provável erro de transcrição" |
| `fiel` (marcado pela Fase 4, confronto não achou) | baixo | ℹ neutro/quieto (cinza): "conferido: sem problema doutrinário aparente" |
| (sem confronto ainda) | médio | ⚠ âmbar genérico (comportamento atual) |

O operador continua vendo **todos** os marcados — mas sabe **onde olhar primeiro**, e em
dois casos sabe **o que fazer** (estender o corte; conferir o texto). É isso que dá
utilidade real ao confronto: dirigir a atenção e apontar a ação.

## Guarda anti-falso-positivo (obrigatória)

Um modelo instruído a "achar o erro" tende a **inventar** desalinhamento onde não há — e um
**motivo citável ERRADO engana o pastor mais** do que um score baixo sem explicação. Por
isso o prompt do confronto DEVE declarar explicitamente:

- que **`fiel` é o resultado esperado e frequente**;
- que a **maioria dos trechos marcados tende a estar correta**, ou apenas **garbled pelo
  ASR** (erro de transcrição, não de doutrina);
- que **não se deve forçar** a identificação de um desalinhamento; na dúvida, classificar
  como `fiel`, `ambiguo_isolado` ou `provavel_erro_transcricao` — **nunca** `desalinhamento`;
- que `desalinhamento` exige **citar o ponto específico** da Declaração que é contrariado —
  sem ponto citável, não é desalinhamento;
- **(por causa do contexto) que o veredito é sobre o TRECHO, não sobre o sermão.** Com
  contexto grande, o modelo tende a julgar o sermão inteiro — e aí um sermão são "absolve"
  qualquer recorte. A instrução tem que ser explícita: o contexto serve **apenas** para
  desambiguar a intenção do pregador; o que vai ao ar é o TRECHO, sozinho, sem o contexto
  ao redor. Um trecho **resolvido no contexto** NÃO é `fiel` — é `ambiguo_isolado`, porque
  publicá-lo como está ainda é um problema (de corte).

## Critérios de aceite

- [ ] Fase nova roda **só** nos trechos `requer_revisao_reforcada = true`; nenhum marcado
      → no-op (zero chamadas ao modelo).
- [ ] **A entrada inclui o CONTEXTO** (±60–90 s antes e depois), não só o trecho — é o que
      diferencia esta fase da Fase 4. Nas bordas do sermão, usa o que houver.
- [ ] Cada trecho marcado gera `{classe, ponto_citado, motivo}` válido (retry da spec-08
      cobre formato), com `classe` nas QUATRO opções.
- [ ] `motivo_revisao` é enriquecido conforme a classe; `requer_revisao_reforcada`
      **permanece true** em todos os casos (nunca limpa, nunca descarta).
- [ ] O candidato carrega a **classe do confronto** (`classe_revisao`), que dirige a
      exibição (spec-05): `desalinhamento` = alto; `ambiguo_isolado` = médio com AÇÃO
      ("estender o corte"); `provavel_erro_transcricao` e `fiel` = baixo (ℹ "conferido: …").
- [ ] Sinalizados persistidos em `trabalho/<id>/revisao-teologica.json`.
- [ ] Prompt contém a guarda anti-falso-positivo, **incluindo** a instrução explícita de que
      o veredito é sobre o TRECHO e não sobre o sermão (o contexto só desambigua).
- [ ] **`onde_resolve` obrigatório em `ambiguo_isolado`** (frase copiada do contexto + lado);
      se o modelo devolver a classe sem a frase, é formato inválido → retry (spec-08).
- [ ] **A viabilidade da extensão é calculada em CÓDIGO** (localiza a frase no `Frasear`,
      compara com start/end, checa a faixa de 30–58 s) — o modelo NÃO decide se cabe.
- [ ] **Teste — ambíguo isolado, resolvido no contexto** (o trecho sozinho sugere algo que o
      pregador esclarece logo em seguida): classe esperada **`ambiguo_isolado`**, **nunca**
      `desalinhamento` — e **nunca** `fiel` (publicar como está ainda é problema, de corte).
      Com `onde_resolve` preenchido.
- [ ] **Teste — extensão CABE**: a frase que resolve está logo à frente → mensagem manda
      estender e cita a frase (e o total estimado fica em 30–58 s).
- [ ] **Teste — extensão NÃO cabe**: a frase que resolve está longe (o trecho estendido
      passaria de 58 s) → mensagem diz que **não dá para consertar por extensão** (reprovar
      ou aceitar ciente), NUNCA "estenda".
- [ ] **Teste — trecho garbled pelo ASR** (ex.: termo hebraico corrompido tipo "chiva ou
      chuva", mensagem no geral correta): classe esperada `provavel_erro_transcricao`,
      **nunca** `desalinhamento`.
- [ ] **Teste — trecho fiel** (doutrina sã, sem ruído, contexto coerente): classe esperada
      `fiel`, **sem acusação inventada** (`ponto_citado` vazio).
- [ ] (Opcional, registrar) Teste — trecho que contradiz a Declaração **mesmo com o
      contexto**: classe `desalinhamento` com `ponto_citado` preenchido.
- [ ] `go build ./...` e `go test ./...` verdes (testes com modelo fake, sem subir o LLM).

## Como validar

```bash
go test ./internal/harness/   # testes da Fase 6 com ModeloLLM fake:
                              #  - ambíguo isolado (contexto resolve) -> ambiguo_isolado
                              #  - garbled -> provavel_erro_transcricao
                              #  - fiel    -> fiel, sem ponto_citado
                              #  - erro real -> desalinhamento + ponto
# Real (com llama-server no ar), num sermão que tenha trecho marcado:
go run ./cmd/harness -transc trabalho/<id>/transcricao.txt -ate 5
# conferir o motivo_revisao enriquecido e o revisao-teologica.json gerado.
```

## Nota (ressalva honesta, registrar no produto)

O confronto é **modelo confrontando modelo**: melhora o sinal com uma lente focada e reduz
falso negativo, mas **não** adiciona uma fonte de verdade independente. Preferimos falso
positivo a deixar passar algo ruim, mas por isso mesmo a guarda anti-falso-positivo existe —
um motivo citável errado é pior que nenhum. O veredito **enriquece**, nunca certifica nem
descarta. A certificação teológica final é sempre humana (regra inviolável nº 6).
