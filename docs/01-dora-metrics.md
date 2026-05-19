# 01 — DORA Metrics: fundamentos

## Origem e propósito

DORA (DevOps Research and Assessment) é um programa de pesquisa iniciado por Nicole Forsgren, Jez Humble e Gene Kim, hoje mantido pelo Google Cloud. O time publica anualmente o "Accelerate State of DevOps Report" desde 2014, que correlaciona práticas de engenharia com **performance de entrega de software** e **performance organizacional**.

A premissa central, validada estatisticamente em milhares de respondentes: times que entregam **mais rápido** e **com mais qualidade** produzem melhor resultado de negócio. Para tornar essa premissa mensurável, DORA define quatro métricas — as **"4 Key Metrics"**.

Mais recentemente (2021+) o programa adicionou uma quinta métrica de **Reliability** (disponibilidade percebida), mas a base segue sendo as quatro originais. Este documento foca nas quatro; a quinta é mencionada como extensão opcional na seção [Métricas auxiliares e v2](#m%C3%A9tricas-auxiliares-e-v2).

## As 4 métricas

As duas primeiras medem **velocidade** (throughput), as duas últimas medem **estabilidade**. A insight original do DORA é que esses dois eixos **não são opostos** — times Elite são fortes nos dois ao mesmo tempo.

### 1. Lead Time for Changes (LTC)

**Definição:** tempo entre o primeiro commit de uma mudança e o momento em que essa mudança roda em produção.

**Por quê:** mede a eficiência do fluxo de valor desde o desenvolvimento até a entrega. Lead time alto indica gargalos (revisão de código, fila de QA, janelas de deploy, branches longas).

**Como calcular (operacionalmente):**

```
LTC(mudança) = timestamp_deploy_producao - timestamp_primeiro_commit
```

Para uma janela de tempo (ex: últimos 30 dias), reportamos a **mediana** das mudanças deployadas naquela janela. **Não use média** — a distribuição é fortemente assimétrica à direita (long tail de PRs antigos que ficam parados).

**Unidade de medida do DORA:** os benchmarks oficiais usam categorias (`< 1 hora`, `< 1 dia`, `< 1 semana`, `< 1 mês`, `> 6 meses`). No nosso produto, vamos guardar em segundos e exibir formatado.

**Decisões de implementação:**

- **O que é "primeiro commit"?** O commit mais antigo no histórico do merge request **que ainda não estava em main**. Para MRs feitos com rebase/squash, usamos o commit original (antes do squash) via API GitLab (`merge_request.commits`).
- **O que é "rodou em produção"?** Ver [Decisão D3 em docs/README.md](README.md#decis%C3%B5es-abertas). Provisoriamente: o primeiro `deployment` na API GitLab cujo `environment.name` case com pattern `production*` e cujo `status == success`.
- **Mudanças cancelaras?** MRs fechados sem merge são ignorados.

### 2. Deployment Frequency (DF)

**Definição:** com que frequência o time faz deploy em produção.

**Por quê:** proxy direto de tamanho de lote. Times que fazem deploy frequente trabalham com batches pequenos, o que reduz risco e acelera feedback. Times que deployam mensalmente acumulam mudanças grandes e arriscadas.

**Como calcular:**

```
DF(janela) = count(deployments_producao_com_sucesso) / dias_da_janela
```

Mas a forma como o DORA Report comunica não é "deploys por dia" e sim **categorias**:

| Categoria             | Frequência típica           |
| --------------------- | --------------------------- |
| On-demand (Elite)     | múltiplos deploys por dia   |
| Entre 1×/dia e 1×/sem | High                        |
| Entre 1×/sem e 1×/mês | Medium                      |
| Menos que 1×/mês      | Low                         |

**Decisões de implementação:**

- **Cada commit em main = deploy?** Não. Conta apenas o evento de _deployment_ efetivo em ambiente de produção (não o merge em main).
- **Deploys de hotfix contam?** Sim, contam como deploy normal.
- **Múltiplos deploys do mesmo artefato (rollback + roll-forward)?** Cada chegada em produção bem-sucedida conta uma vez. Falhas não contam.
- **Múltiplos serviços/projetos?** Reportamos DF **por projeto** e também agregada. Não somar produção de produtos diferentes em um número só sem dimensão.

### 3. Change Failure Rate (CFR)

**Definição:** percentual de deploys em produção que resultaram em **falha que precisou de remediação** (rollback, hotfix, patch).

**Por quê:** mede a qualidade do processo de entrega. CFR alto indica que velocidade está vindo às custas de estabilidade. CFR baixo + DF alto é o sweet spot do Elite.

**Como calcular:**

```
CFR(janela) = deploys_falhos / total_deploys × 100%
```

**O denominador é o total de deploys daquela janela** (não o total de mudanças, não o total de incidentes). O numerador é quantos deploys causaram problema.

**Decisões de implementação (críticas):**

- **O que é "deploy falho"?** Ver [Decisão D4 em docs/README.md](README.md#decis%C3%B5es-abertas). Estratégias possíveis:
  1. **Issue marcada em Jira:** issue de tipo `Incident` ou label `production-incident` criada dentro de N horas após o deploy.
  2. **Rollback explícito:** deployment cujo `environment` voltou para uma SHA anterior dentro de N horas.
  3. **Hotfix:** MR com label `hotfix` em produção dentro de N horas após o deploy original.
- **Causa vs correlação:** se 3 incidentes são criados em 24h de um deploy, isso é 1 falha (não 3). Atrelar incidentes ao deploy "culpado", não inflar o numerador.

### 4. Mean Time to Restore / Recover (MTTR)

**Definição:** tempo médio entre o início de um incidente em produção e a sua resolução.

**Por quê:** mede capacidade de **recuperação**. Falhas vão acontecer; o que diferencia times Elite é restaurar rápido.

**Como calcular:**

```
MTTR = média(timestamp_resolução - timestamp_início) dos incidentes da janela
```

Atenção: aqui o DORA usa **média** mesmo (não mediana), mas na prática vale reportar ambas. Uma mediana baixa com um outlier de 72h é um sinal de cauda longa que a média captura.

**Decisões de implementação:**

- **Início do incidente:** `created_at` da issue Jira de incidente, OU `started_at` de um evento de pager. Idealmente o mais cedo possível (quando o usuário foi impactado), não quando o time descobriu.
- **Fim do incidente:** `resolved_at` da issue / mudança de status para "Done"/"Closed". Não usar `updated_at` — falsos positivos.
- **Incidentes contínuos cruzando janelas:** atribuir ao período em que o incidente **terminou** (assim a métrica reflete capacidade real de recuperação).

## Benchmarks Elite / High / Medium / Low

Os benchmarks variam entre os relatórios anuais e o produto deve permitir configurar os limiares. Os valores abaixo refletem a faixa publicada nos relatórios DORA recentes (Accelerate State of DevOps 2023/2024) — **use como ponto de partida, não como verdade fixa**:

| Métrica                  | Elite                       | High                       | Medium                     | Low                       |
| ------------------------ | --------------------------- | -------------------------- | -------------------------- | ------------------------- |
| **Deployment Frequency** | On-demand (várias/dia)      | Entre 1×/dia e 1×/semana   | Entre 1×/semana e 1×/mês   | Menos de 1×/mês           |
| **Lead Time**            | < 1 hora                    | Entre 1 dia e 1 semana     | Entre 1 semana e 1 mês     | Entre 1 mês e 6 meses     |
| **Change Failure Rate**  | 0–5%                        | 10%                        | 15–20%                     | 30–40%                    |
| **MTTR**                 | < 1 hora                    | < 1 dia                    | Entre 1 dia e 1 semana     | > 1 semana                |

> Observação: o relatório 2023 fundiu Elite e High em algumas dimensões — o produto deve suportar tanto a taxonomia de 4 níveis quanto a de 3 níveis (Elite/Medium/Low) como configuração.

## Armadilhas comuns

Erros que destroem a credibilidade das métricas. O time precisa conhecer todos:

1. **Vanity metrics:** otimizar para "deploy frequency alta" mergeando MRs vazios. Mitigação: cruzar DF com tamanho de mudança (linhas alteradas) e cobertura de testes.

2. **Esconder falhas:** medir CFR a partir de issues abertas pelos próprios desenvolvedores ⇒ incentivo perverso a não abrir incidentes. Mitigação: usar fonte de dados que não depende da decisão do desenvolvedor (sinais automáticos, alarmes de SLO).

3. **Comparar times incomparáveis:** time de plataforma vs time de produto de cara nova têm dinâmicas diferentes. Mitigação: comparar **um time consigo mesmo ao longo do tempo**, não rankings transversais.

4. **Definição inconsistente de "produção":** equipes diferentes chamando ambientes diferentes de "production". Mitigação: catálogo central de ambientes por projeto (parte do nosso modelo de dados — ver [06-data-model.md](06-data-model.md)).

5. **Janela móvel curta demais:** medir DF semanal em time que deploya 2×/mês resulta em ruído. Mitigação: usar janelas rolantes de 30/90 dias e mostrar tendência, não pontos isolados.

6. **Misturar "merge em main" com "deploy em produção":** em times com deploy contínuo isso converge, mas em times com janela de deploy semanal, são eventos distintos. Mitigação: separar `merge_event` de `deployment_event` no modelo de dados.

7. **Ignorar bots:** PRs do Dependabot/Renovate inflam DF artificialmente se contados como deploy. Mitigação: filtro por autor configurável.

## Métricas auxiliares e v2

Além das quatro, o produto deve suportar como extensões opcionais:

- **Reliability** (DORA v2, 2021+): combinação de disponibilidade percebida, latência e satisfação operacional. Mais subjetiva — coleta via survey ou integração com Datadog/Grafana SLO.
- **Tamanho de mudança:** linhas adicionadas/removidas por MR. Sinal de batch size.
- **Code Review Time:** tempo entre `MR opened` e `MR approved`. Componente do Lead Time, útil para identificar gargalo.
- **Pickup Time:** tempo entre `MR opened` e `primeira review`. Indicador de capacidade de revisão.
- **Throughput de issues:** issues Jira fechadas por unidade de tempo, comparado a issues abertas.

Essas métricas auxiliares não são "DORA" stricto sensu, mas são amplamente reportadas em dashboards modernos (ex: LinearB, Sleuth, Faros AI) e ajudam no diagnóstico quando uma métrica DORA principal piora.

## Periodicidade de cálculo

- **Real-time (event-driven):** quando um webhook chega (deploy, MR merged, incident resolved), publicamos um evento que atualiza agregações incrementais.
- **Recompute diário:** job noturno recalcula janelas rolantes de 7/30/90 dias para corrigir eventos atrasados.
- **Snapshot mensal:** congela o valor das 4 métricas no fim do mês para histórico imutável.

Detalhes em [05-architecture.md](05-architecture.md) e [06-data-model.md](06-data-model.md).

## Fontes

- Forsgren, N., Humble, J., Kim, G. *Accelerate: The Science of Lean Software and DevOps* (2018).
- Google Cloud — Accelerate State of DevOps Report (publicações anuais 2019–2024).
- DORA Quick Check — https://dora.dev/quickcheck/ (ferramenta oficial de autoavaliação; mantida por Google Cloud).

> Esta seção foi escrita com base em conhecimento consolidado. Quando o relatório DORA 2025 (referente a dados de 2024) for publicado, revisar os limiares na tabela de benchmarks.
