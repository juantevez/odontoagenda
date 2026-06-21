# =============================================================================
# OdontoAgenda — nats-init container
#
# Usa la imagen oficial nats-box que incluye la CLI `nats` preinstalada.
# Es una imagen de un solo propósito: ejecutar init-streams.sh y salir.
# No corre ningún daemon; el contenedor termina con exit 0 si todo va bien.
# =============================================================================
FROM natsio/nats-box:latest

COPY init-streams.sh /init-streams.sh
RUN chmod +x /init-streams.sh

ENTRYPOINT ["/init-streams.sh"]
