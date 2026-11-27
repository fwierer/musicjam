// Bounded client-side adapter for the Integrated YouTube Player context.
// Manages the official YT.Player IFrame lifecycle, wires the host-only Skip button,
// and automatically skips songs that cannot be played in embedded players (e.g. copyright blocks).

function createHostPlayer({
  startButton,
  skipButton,
  statusEl,
  titleEl,
  metaEl,
  frameEl,
  onFinishSong,
  onSkipSong,
}) {
  let player = null;
  let playerReady = false;
  let apiReady = false;
  let started = false;
  let currentSong = null;
  let currentSongId = "";
  let playingSongId = "";
  let finishedSongId = "";
  let busyAdvancing = false;
  let sawBuffering = false;
  let hadErrorBeforeReady = false;
  let stallTimer = null;
  let noticeTimer = null;

  function clearStallTimer() {
    if (stallTimer) {
      clearTimeout(stallTimer);
      stallTimer = null;
    }
  }

  function showNotice(message, durationMs = 7000) {
    if (noticeTimer) {
      clearTimeout(noticeTimer);
      noticeTimer = null;
    }
    statusEl.textContent = message;
    if (message && durationMs > 0) {
      noticeTimer = setTimeout(() => {
        noticeTimer = null;
        if (statusEl.textContent === message) {
          statusEl.textContent = "";
        }
      }, durationMs);
    }
  }

  function destroyPlayer() {
    clearStallTimer();
    if (player) {
      try {
        player.destroy();
      } catch {
        // Ignore teardown errors from broken IFrame instances
      }
      player = null;
    }
    playerReady = false;
    hadErrorBeforeReady = false;
  }

  function init() {
    window.onYouTubeIframeAPIReady = () => {
      apiReady = true;
      if (currentSong) {
        loadIntoPlayer(currentSong.videoId);
      }
    };
    const script = document.createElement("script");
    script.src = "https://www.youtube.com/iframe_api";
    document.body.append(script);

    startButton.addEventListener("click", () => {
      if (!playerReady || !currentSongId || !player) return;
      started = true;
      startButton.hidden = true;
      player.playVideo();
    });

    if (skipButton) {
      skipButton.addEventListener("click", async () => {
        const targetId = currentSongId || currentSong?.id;
        if (!targetId || busyAdvancing) return;
        clearStallTimer();
        started = true;
        playingSongId = "";
        skipButton.disabled = true;
        try {
          await triggerSkip(targetId, "host", "");
        } finally {
          skipButton.disabled = false;
        }
      });
    }
  }

  async function finishCurrentSong(songId) {
    if (!songId || busyAdvancing) return;
    clearStallTimer();
    finishedSongId = songId;
    playingSongId = "";
    busyAdvancing = true;
    try {
      const removed = await onFinishSong(songId);
      if (removed && finishedSongId === songId) {
        finishedSongId = "";
      }
    } finally {
      busyAdvancing = false;
    }
  }

  async function triggerSkip(songId, reason, noticeMessage) {
    if (!songId || busyAdvancing) return;
    clearStallTimer();
    finishedSongId = songId;
    playingSongId = "";
    busyAdvancing = true;
    if (noticeMessage) {
      showNotice(noticeMessage, 7000);
    }
    try {
      const skipFn = onSkipSong || onFinishSong;
      const removed = await skipFn(songId, reason);
      if (removed && finishedSongId === songId) {
        finishedSongId = "";
      }
    } finally {
      busyAdvancing = false;
    }
  }

  async function handlePlayerError(errorCode) {
    if (!currentSong || currentSong.id !== currentSongId || finishedSongId === currentSongId || busyAdvancing) {
      return;
    }
    if (!playerReady) {
      hadErrorBeforeReady = true;
    }
    const songSnapshot = { ...currentSong };
    console.warn(`YouTube Player error ${errorCode} for video ${songSnapshot.videoId} (${songSnapshot.title})`);

    let message;
    if (errorCode === 101 || errorCode === 150 || errorCode === 153) {
      message = `Skipped "${songSnapshot.title}" — forbidden due to copyright / only playable on YouTube.`;
    } else if (errorCode === 100) {
      message = `Skipped "${songSnapshot.title}" — video is unavailable or private.`;
    } else {
      message = `Skipped "${songSnapshot.title}" — video could not be played.`;
    }

    started = true;
    await triggerSkip(songSnapshot.id, `youtube_error_${errorCode}`, message);
  }

  function loadIntoPlayer(videoId) {
    if (!apiReady) return;

    if (hadErrorBeforeReady) {
      destroyPlayer();
      frameEl.innerHTML = '<div id="player"></div>';
    }

    if (!player) {
      startButton.disabled = true;
      player = new YT.Player("player", {
        videoId,
        playerVars: { playsinline: 1, origin: location.origin },
        events: {
          onReady: (event) => {
            if (event.target !== player) return;
            playerReady = true;
            startButton.disabled = false;
            startButton.hidden = started;
            if (started && player) {
              player.playVideo();
            }
          },
          onStateChange: (event) => {
            if (event.target !== player || !currentSongId) return;
            if (event.data === YT.PlayerState.BUFFERING) {
              clearStallTimer();
              sawBuffering = true;
              started = true;
              startButton.hidden = true;
            } else if (event.data === YT.PlayerState.PLAYING) {
              clearStallTimer();
              sawBuffering = false;
              started = true;
              playingSongId = currentSongId;
              startButton.hidden = true;
            } else if (event.data === YT.PlayerState.PAUSED || event.data === YT.PlayerState.CUED) {
              clearStallTimer();
            } else if (event.data === YT.PlayerState.UNSTARTED && sawBuffering && started) {
              // Catch silent YouTube embed/copyright blocks that drop from BUFFERING back to UNSTARTED (-1)
              const stalledSongId = currentSongId;
              clearStallTimer();
              stallTimer = setTimeout(() => {
                stallTimer = null;
                if (
                  currentSongId === stalledSongId &&
                  started &&
                  player &&
                  typeof player.getPlayerState === "function" &&
                  player.getPlayerState() === YT.PlayerState.UNSTARTED
                ) {
                  handlePlayerError(150);
                }
              }, 3500);
            } else if (event.data === YT.PlayerState.ENDED && playingSongId === currentSongId) {
              clearStallTimer();
              playingSongId = "";
              finishCurrentSong(currentSongId);
            }
          },
          onAutoplayBlocked: (event) => {
            if (event.target !== player) return;
            clearStallTimer();
            started = false;
            startButton.hidden = false;
          },
          onError: (event) => {
            if (event.target !== player) return;
            handlePlayerError(event.data);
          },
        },
      });
    } else if (started) {
      player.loadVideoById(videoId);
    } else {
      player.cueVideoById(videoId);
    }
  }

  function sync(queue) {
    const song = queue[0];
    if (!song) {
      destroyPlayer();
      frameEl.innerHTML =
        '<div id="player" class="d-flex align-items-center justify-content-center text-body-secondary">Waiting for a guest to pick a song</div>';
      currentSong = null;
      currentSongId = "";
      playingSongId = "";
      finishedSongId = "";
      sawBuffering = false;
      startButton.hidden = true;
      if (skipButton) skipButton.hidden = true;
      titleEl.textContent = "Nothing yet";
      metaEl.textContent = "";
      return;
    }

    // Always keep the host Skip button visible whenever there is a song in the queue.
    if (skipButton) skipButton.hidden = false;

    if (busyAdvancing && song.id === finishedSongId) {
      return;
    }

    if (finishedSongId === song.id) {
      finishCurrentSong(song.id);
      return;
    }

    if (song.id === currentSongId) {
      if (apiReady && !player) {
        loadIntoPlayer(song.videoId);
      }
      return;
    }

    if (player && !playerReady && !hadErrorBeforeReady) return;

    clearStallTimer();
    currentSong = song;
    currentSongId = song.id;
    playingSongId = "";
    finishedSongId = "";
    sawBuffering = false;
    titleEl.textContent = song.title;
    metaEl.textContent = song.channel;

    if (apiReady) {
      loadIntoPlayer(song.videoId);
    }
  }

  return {
    init,
    sync,
    isAdvancing: () => busyAdvancing,
  };
}
