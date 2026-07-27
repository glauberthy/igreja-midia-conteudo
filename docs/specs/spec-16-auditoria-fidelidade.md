# Spec 16 — Auditoria de fidelidade dos candidatos (cmd/auditar)

> **Spec retroativa.** Documenta código já implementado e validado no commit `3c4b949`
> (`cmd/auditar: auditoria determinística dos candidatos contra a legenda`), com a grade
> de critérios adicionada depois. Escrita depois do fato para acertar a dívida documental.

## Objetivo

Uma ferramenta **determinística** (sem LLM) que cruza os candidatos já validados de um
pedido com a legenda real e acusa defeitos de **fidelidade** e de **corte** que a seleção
(que é julgamento do modelo) não garante sozinha. É a rede de verificação que responde
"o trecho escolhido corresponde ao que foi realmente falado, e o corte está bem-formado?".

## Contexto

A seleção (harness) é julgamento do modelo; a validação (Fase 5) é uma rede determinística
interna. Faltava uma ferramenta **externa e sob demanda** para o operador/desenvolvedor
conferir os candidatos contra a transcrição a qualquer momento — inclusive os já gravados.

Ela nasceu de uma pergunta do dono ("você cruzou os resultados com a legenda, ou só recebeu
o resultado do modelo?") e, na primeira execução, expôs três coisas reais: o bug do "hook
clipado" da Fase 5 (start deslizado ~3 s — corrigido no commit `024e440`), um pedido com
artefatos de fontes misturadas, e um erro de ASR do YouTube que invertia o sentido ("sem
Cristo" por "em Cristo"). Ver `resultados/auditoria-fidelidade.md`.

## Escopo

Dentro:
- `cmd/auditar`: lê `trabalho/<id>/candidatos.corrigido.json` + `transcricao.txt`, cruza
  cada candidato com as frases da legenda (`harness.Frasear`) e reporta em markdown.
- Invariantes verificadas por candidato:
  1. **hook existe na legenda** e o **`start` cai DENTRO da frase do hook** — entre o começo
     dela e uma folga curta (`harness.FolgaInicioMaxMs`), nunca antes (acusa "start antes da
     frase do hook"
     se o hook começa antes do start, ou "start com sobra" se começa depois; "hook não
     encontrado / inventado" se não casa);
  2. o **`end` não termina ANTES de uma fronteira de frase completa** (não corta fala no
     meio), com folga para frente limitada a `harness.FolgaFimMaxMs` (5 s);
  3. **duração** em 30–60 s.
- Flags de leitura: `-texto` (imprime o texto realmente falado na janela, insumo da
  revisão humana) e `-criterios` (grade dos 5 critérios da Fase 4 — por que o score é
  aquele).

Fora:
- **Não** usa LLM e **não** julga doutrina — a certificação teológica continua humana
  (regra inviolável nº 6). O `-texto`/`-criterios` são insumo para essa revisão.
- Não corrige nada: só reporta (a correção de bugs que ela revela é feita à parte).

### Por que a invariante de início é "cai dentro da frase do hook", e não "Δ=0"

Mesma causa da invariante de fim, e por um tempo tratada como se não valesse ali. A primeira
versão exigia que o hook começasse **exatamente** no `start`. A formulação fiel:

| Onde cai o `start` | Abre com fala alheia? | Veredito |
|---|---|---|
| Antes do começo da frase do hook | **Sim** — pega o rabo da fala anterior | acusa |
| Exatamente no começo da frase | Não | passa |
| Depois do começo, até 5 s | Não (o carimbo adianta o áudio: ali a fala ainda não começou) | passa |
| Depois do começo, além de 5 s | Não, mas abre no meio da frase de abertura | acusa |

O caso medido pelo operador: corte em `00:20:08` e ainda se ouvia `"...do pelo Senhor"`, o rabo
de uma frase carimbada em `00:20:05`. Se os carimbos fossem exatos, ela teria acabado antes de
`00:20:08`. Ouvi-la depois é prova direta do adiantamento — a frase seguinte, marcada em
`00:20:08`, só é falada por volta de `00:20:10`.

Com Δ=0 obrigatório, o operador ficava **sem saída**: clicava "mais tarde", ia para `00:20:09`,
e o encaixe na fronteira mais próxima o devolvia para `00:20:08`. O botão não fazia nada
visível. Era o mesmo argumento que já havia liberado o fim — a diferença é que na época se
supôs que "o início deve ser exato", e o defeito da fonte não faz essa distinção.

**Uma diferença importante em relação ao fim:** o hook continua sendo a frase que **contém** o
`start`, não a seguinte. Com `start` em `00:20:10`, o hook segue sendo `"Todo cristão deve
estar preparado…"` — que é o que se ouve — e não pula para a frase de depois. Na faixa de
frases da tela, essa frase continua destacada como dentro do corte, e o texto falado começa
nela. Se o hook pulasse, a tela mostraria uma abertura que não corresponde ao áudio.

Começar **antes** do começo da frase continua sendo defeito, pelo carimbo e pelo áudio: aí o
Short abre mesmo com a fala anterior. E a folga tem teto, senão o Short abriria no meio da
frase de abertura.

### Por que a invariante de fim é "não termina antes", e não "termina exatamente em"

A primeira versão exigia `end == fim de frase completa`. A formulação correta é mais frouxa
de um lado e igualmente firme do outro:

| Onde cai o `end` | Corta fala? | Veredito |
|---|---|---|
| Antes de qualquer fronteira | **Sim** | acusa |
| Exatamente na fronteira | Não | passa |
| Depois da fronteira, até 5 s | Não (silêncio ou início da fala seguinte) | passa |
| Depois da fronteira, além de 5 s | Não, mas vaza conteúdo | acusa |

O motivo é um defeito medido da fonte: **o timestamp da legenda automática do YouTube
adianta o áudio em 1–3 s**. O texto da frase vem completo, mas o tempo de fim chega antes de
o pregador terminar de falar. Cortar exatamente na fronteira, então, ENGOLE a palavra final —
é o defeito que o operador escuta como `"...fez por nós,"` faltando `"preço nenhum paga"`.

Terminar depois da fronteira nunca corta fala: pega silêncio ou o começo da fala seguinte, e
o quanto disso é aceitável é julgamento de ouvido — por isso é o operador que decide, dentro
do teto. Sem teto, a folga viraria vazamento silencioso.

Isto **não é exceção aberta para o ajuste manual** (spec-05 v2): é o enunciado mais fiel da
intenção original do auditor, que sempre foi "não cortar fala no meio". A alternativa —
manter a igualdade exata e ensinar o auditor a ignorar trechos ajustados à mão — criaria uma
classe de material que o projeto marca como defeituoso e manda ignorar, o que é pior que o
problema. A delimitação automática (Fase 3) continua produzindo `end` exatamente na
fronteira e continua passando sem folga nenhuma.

## Contrato e uso

```bash
go run ./cmd/auditar -id <pedido>            # audita um pedido
go run ./cmd/auditar -todos                  # todos os pedidos com candidatos em trabalho/
go run ./cmd/auditar -id <pedido> -texto     # inclui o texto falado de cada trecho
go run ./cmd/auditar -id <pedido> -criterios # grade de critérios da Fase 4
go run ./cmd/auditar -todos > resultados/auditoria.md   # salvar o relatório
```

Flags: `-id`, `-todos`, `-base` (padrão `trabalho`), `-texto`, `-criterios`.

Saída: markdown por pedido — `## <id> — N candidato(s)` e, por candidato, `status`
(✅ fiel ou ⚠ com a lista de problemas), o hook e, opcionalmente, o texto falado e a grade.
**Código de saída ≠ 0** se encontrar algum problema (útil em verificação automatizada).

## Critérios de aceite

- [x] Determinístico, sem LLM; lê os artefatos do pedido em `trabalho/<id>/`.
- [x] Acusa hook clipado, hook inventado, end que corta fala no meio, folga de fim excessiva
      e duração fora de 30–60 s.
- [x] `-texto` imprime o texto falado; `-criterios` imprime a grade da Fase 4.
- [x] Sai com código ≠ 0 quando há problema.
- [x] `go build ./...` e `go test ./...` verdes.

## Como validar

```bash
go test ./cmd/auditar/    # TestAuditarCandidato* (fiel, hook clipado, hook inventado,
                          # end no meio da fala, duração fora) + TestGradeCriterios
go run ./cmd/auditar -todos
```

## Quando rodar

- **Depois de uma seleção, antes de renderizar/entregar** — pega corte mal-formado antes
  de virar vídeo.
- Ao **investigar uma suspeita** (trecho estranho, legenda esquisita): `-texto` mostra o
  que foi realmente falado.
- Em **verificação automatizada** (o código de saída ≠ 0 sinaliza problema).
- Ao mexer na Fase 3/5 (delimitação/validação): é o teste de fumaça contra dados reais.
