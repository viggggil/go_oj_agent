n,t=map(int,input().split()); a=list(map(int,input().split())); seen={}
for j,x in enumerate(a,1):
    if t-x in seen:
        print(seen[t-x],j); break
    seen[x]=j
