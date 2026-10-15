from collections import deque
n=int(input()); g=[[] for _ in range(n)]
for _ in range(n-1):
    u,v=map(int,input().split()); g[u-1].append(v-1); g[v-1].append(u-1)
def far(s):
    d=[-1]*n; d[s]=0; q=deque([s])
    while q:
        u=q.popleft()
        for v in g[u]:
            if d[v]<0: d[v]=d[u]+1; q.append(v)
    u=max(range(n),key=lambda x:d[x]); return u,d[u]
print(far(far(0)[0])[1])
