# Architecture implémentée et contrats V1

## Chemin d'exécution

1. Le CLI soumet un manifest au serveur de zone source, via MQTT5 RPC `UPLOAD`.
2. Le serveur consulte le Central pour réconcilier un request_id déjà committé.
3. Le noyau groupe les segments entiers en extents contigus. Un segment plus
   grand que la cible reste seul ; la cible n'est pas une limite absolue.
4. La zone source réserve atomiquement le sous-plan local. Son mutex protège
   l'ensemble du calcul et de la réservation, pas seulement une copie virtuelle.
5. S'il reste des extents : résumés frais des voisins configurés, OFFER en lot
   simultané (fan-out 2 par défaut), puis RESERVE ; les batches suivants peuvent
   ajouter d'autres zones. Le noyau accepte au maximum cinq voisins ; le banc
   fournit A/B et ses ACL/autorisations sont volontairement limitées à ces zones.
6. Chaque zone distante exécute son HRW sur son registre local. Le source ne voit
   que le résumé agrégé et le plan de réponse ; il n'inspecte aucun registre distant.
7. Le Central valide et commit le plan RESERVED avec son event_id, dans une même
   transaction SQLite. Sa réponse métier vient après COMMIT.
8. Le scénario groupe les commandes par nœud. Les mocks débitent leur allocation
   projet et émettent STORE_COMPLETE **simulés**. Le Central valide node_id,
   extent_id, reservation_id et l'expiration avant de passer l'extent à STORED.
9. READY n'est committé qu'au dernier extent attendu. Le scénario marque alors
   les réservations de zone COMMITTED (capacité consommée, non libérable par RELEASE).

L'API pure `Plan` produit seulement PLANNED. `Zone.Reserve` accepte RESERVED.
Un résultat de transfert est un événement, pas un plan. Le succès d'un Publish
MQTT n'atteste aucun COMMIT SQLite. Pas de vidéo, de codec, de SIS, de RTP/RTX ou
de WebRTC dans ces chemins. `LivePolicy` est seulement un point d'extension.

## Manifest et HRW

IDs texte : ASCII alphanumérique puis `._-`, 1 à 128 caractères. Segment_id
numérique, séquence zéro-indexée strictement contiguë ; zéro octet interdit.
Maximum 1 000 000 segments ; tailles et somme limitées à MaxInt64 pour SQLite.
Les décodeurs JSON rejettent champs inconnus, types invalides et données après le
premier document. Les nombres sont décodés directement dans des entiers Go.
Le transport contrôle limite chaque payload à 1 MiB. `ExternalManifestResolver`
définit l'extension hors MQTT ; aucun fetch HTTP arbitraire n'est implémenté.

HRW : SHA-256 de trois chaînes (video_id, extent_id décimal canonique, node_id),
chacune préfixée par sa longueur uint64 big-endian. Les 52 bits supérieurs du
digest forment `U=(bits+0.5)/2^52`, strictement dans `(0,1)`. Score
`-ln(U)/available_bytes`, minimum gagnant. Identités triées pour départager une
égalité ; priorité AVAILABLE, puis DEGRADED si aucun AVAILABLE n'accueille
l'extent, jamais UNAVAILABLE. Snapshot et heartbeat frais obligatoires.

Les poids diminuent après chaque extent ; aucune capacité virtuelle négative.
Le résultat est déterministe pour les mêmes entrées ordonnées/snapshots. Il ne
promet ni répartition instantanée exacte ni remapping minimal à la manière de
CRUSH. Les plans impossibles renvoient explicitement les extents restants. Une
zone avec 100 octets répartis en deux nœuds de 50 ne peut accueillir un extent de
80, même si son OFFER agrégé en octets semble suffisant.

## Capacité : un seul compteur net

`available_for_project_bytes` est libre pour le projet, **déjà net des
réservations comptabilisées par l'Edge au snapshot**. `included_reservation_ids`
liste les débits locaux déjà inclus. La zone soustrait seulement les allocations
de son ledger qui ne figurent pas dans cette liste. Elle ne soustrait jamais
aveuglément un deuxième champ reserved_bytes. Après RELEASE d'un débit inclus,
le nœud est temporairement inéligible jusqu'à un nouveau snapshot net.

Chaque ledger de zone est checkpointé par fichier JSON, fsync fichier, rename
atomique puis fsync répertoire, **avant** de répondre RESERVE/RELEASE/COMMIT.
Une erreur de persistance ferme l'admission. Les fichiers sont dans le volume
de la zone, distincts de la DB locale de Dan. Le noyau ne dépend pas du disque :
il reçoit une fonction sink. Après restart, réservations et tombstones sont
restaurés, mais **pas la présence des nœuds**. Les tests vérifient cette reprise.
Le lab doit recevoir un vrai heartbeat non-retained avant toute admission.
Les générations de résumé combinent l'époque de démarrage et les mutations ;
une autorité persistante de génération à l'échelle de l'équipe reste à définir.

Expiration bornée à dix minutes maximum, défaut lab 60 secondes. OFFER ne
réserve rien. RESERVE déduplique sur reservation_id et contenu immuable, TTL
inclus ; un retry conserve exactement le même corps. RELEASE est idempotent et
garde un tombstone contre les RESERVE retardés. Pas de réactivation d'un ID
expiré/libéré. Au maximum 10 000 entrées de ledger et 32 MiB checkpoint : au-delà,
l'admission échoue explicitement et demande une réconciliation/purge contrôlée.

## Réseau et classement inter-zone

Les résumés portent zone_id, generation, updated_at, santé et capacité nette.
Les métriques de chemin sont des entrées **mesurées par la source A vers B**,
distinctes de la santé de B. RTT/goodput absents restent nil. Quand les deux sont
connus, classement par `RTT + bytes/goodput`, après santé ; sinon ordre de zone
stable. Aucun nombre synthétique n'est annoncé comme une mesure ou une QoS.

Timeout par RPC ; OFFER en parallèle dans un batch, RESERVE réévalue ensuite les
capacités atomiquement. Une réponse distante doit partitionner exactement les
extents demandés. En cas de résultat de RESERVE perdu, RELEASE emploie son ID
connu. Si RELEASE est lui-même impossible, l'expiration borne le débit. Un
échec final libère les réservations encore ouvertes des zones contactées.

## MQTT5 et autorisation de référence

Topics isolés :

```text
lab/placement/v1/{receiver}/request/{sender}
lab/placement/v1/{receiver}/reply/{sender}
lab/placement/v1/zone/{zone}/node/{node}/state
lab/placement/v1/zone/{zone}/node/{node}/heartbeat
lab/placement/v1/zone/{zone}/summary
lab/placement/v1/presence/{identity}
```

Le dernier composant RPC est lié à `%u` par ACL Mosquitto. Response Topic doit
correspondre à l'auteur et au destinataire ; Correlation Data est un aléa de
128 bits et la réponse est vérifiée contre le destinataire attendu. Enveloppe
`op/body/expires_at`. Les identités des événements node/STORE_COMPLETE sont
comparées à l'auteur authentifié ; PLAN/CANCEL vérifient la zone source.
64 RPC simultanés et 64 handlers au maximum ; pas de file mémoire illimitée.

Commandes/événements QoS1 et non-retained, heartbeat QoS0 non-retained. State,
summary et LWT sont retenus ; Message Expiry 5 secondes pour les snapshots lab,
validation timestamp/version côté application. Un retained AVAILABLE ne crée
pas de présence sans heartbeat reçu par la zone. Summary est coalescé par
intervalle configurable, défaut 250 ms, avec canal prioritaire pour les états
critiques et actualisation périodique. Pas de publication par segment.

Le broker lab utilise des comptes et ACL, sans port hôte. Son secret de démo
est commun aux comptes **uniquement dans le banc isolé** ; ce n'est pas un modèle
d'identités de production. TLS, mots de passe distincts/certificats, ownership et
arbre de topics de l'équipe doivent être convenus avant raccordement réel.
Les endpoints HTTP exposent uniquement la santé ; aucune mutation HTTP ouverte.

## Catalogue, redelivery et reprises

SQLite : foreign_keys ON, WAL vérifié, busy_timeout=5000, connexion writer unique.
Tables nodes/videos/extents/processed_events/upload_requests et index node/state.
Un event_id déjà committé renvoie son résultat d'origine ; même ID avec autre
contenu est une erreur. Échec en milieu d'événement : rollback de toutes les
mutations et de l'event_id. Les tests injectent un abort au deuxième extent et
rouvrent la DB pour vérifier la persistance après commit.

La source retente trois fois la même opération métier après réponse perdue.
LOOKUP retrouve le manifest et le plan immuables d'un request_id après restart
ou réponse PLAN perdue ; il vérifie les données soumises et la granularité.
Aucun nouveau débit n'est accepté si le Central est inaccessible. Un résultat
PLAN encore inconnu laisse les leases expirer et rend un message de
réconciliation explicite. Un plan RESERVED expiré n'est jamais relancé comme un
nouvel upload ; utiliser une nouvelle identité après traitement de l'échec.

Mobilité : seule nodes.zone_id change pour une generation strictement plus
grande. Extents conserve node_id. Les événements anciens deviennent sans effet.
Le mock demande d'abord DEPART à la source, puis annonce une génération supérieure
au Central et un snapshot net à la destination. Ce scénario est testé ; un vrai
handover interrompu, l'autorité de génération, les reprises des transferts et
les suppressions partielles restent contractuels, pas réputés intégrés.

Les mocks sont en mémoire : leur restart ne prouve pas une reprise de stockage
réel. Le restart Central et la conservation du ledger de zone sont testés
séparément. Le module legacy de Dan et les Edge réels n'utilisent pas encore le
contrat projet/net/included ; leurs interfaces sont dans OPEN_QUESTIONS.
