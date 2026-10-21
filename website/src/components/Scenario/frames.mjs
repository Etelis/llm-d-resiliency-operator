export function frames(data) {
  let pods = (data.pods || []).map((pod) => ({...pod, members: {...pod.members}}));
  return data.steps.map((step) => {
    pods = pods.map((pod) => {
      const change = (step.pods || {})[pod.name];
      if (!change) return pod;
      const {uid, gone, ...members} = change;
      return {
        ...pod,
        uid: uid || pod.uid,
        gone: gone === undefined ? pod.gone && !uid : gone,
        members: {...pod.members, ...members},
      };
    });
    return {...step, pods};
  });
}
